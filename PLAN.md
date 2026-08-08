# Plan d'action — vcontrold-go

Établi le 8 août 2026, après revue du code et comparaison avec l'implémentation
de référence en C (`openv/vcontrold`, HEAD `8ca4797`).

---

## Contexte

Lecture et pilotage d'une chaudière **Viessmann Vitola 200 VB2A** équipée d'une
régulation **Vitotronic 200 KW2**, via l'interface optique **Optolink** et le
protocole **KW2**.

| | |
|---|---|
| Machine de développement | le poste de travail |
| Machine cible, sonde branchée | l'hôte où l'Optolink USB est connecté |
| Broker MQTT | racine de topics `openv/` |
| Home Assistant | pour la vérification des `state_class` |

*Les adresses réelles sont dans `.env`, non versionné.*

Workflow : on développe ici, on déploie et on exécute sur la cible
(`make deploy`, `make run-remote`).

**Objectif métier prioritaire** : récupérer les **heures de brûleur**
(`getBrennerStunden1`) et les **démarrages** (`getBrennerStarts`). Ces deux
compteurs sont la dernière inconnue d'un bilan énergétique qui repose
aujourd'hui sur une hypothèse de rendement de chaudière à 0,87. Ils
conditionnent le dimensionnement d'une pompe à chaleur à ~20 000 €.

---

## Ce qui est déjà correct — ne pas y toucher

**Le protocole de lecture KW2 est conforme à la référence C.** Vérifié octet
par octet contre les macros du XML communautaire :

```xml
<macro name="SYNC">    <command>SEND 04;WAIT 05</command></macro>
<macro name="GETADDR"> <command>SEND 01 F7</command></macro>
<command name="getaddr"><send>SYNC;GETADDR $addr $hexlen;RECV $len $unit</send></command>
```

`SerialCommunicator.ReadFromDevice` fait exactement cela. Les paramètres série
(4800 bauds, 8N2, parité paire) sont bons. `evaluateCalc` gère correctement
`V/3600` (unité `CS`, heures de brûleur).

**Les données déjà collectées sont fiables** : les températures et compteurs
publiés en septembre-octobre 2025 étaient corrects.

---

## Tâche 1 — Corriger le chemin d'écriture (bloquant)

`internal/viessmann/serial_communicator.go`, fonction `sendWriteCommand`.

**Trois défauts**, l'écriture ne peut pas fonctionner :

| Actuel | Attendu (référence C) |
|---|---|
| 2\ :sup:`e` octet = `0x01` | **`0xF4`** (`SETADDR = SEND 01 F4`) |
| Ajoute un checksum XOR | **Aucun checksum en KW** (c'est du P300) |
| Attend `0x05` en réponse | **`RECV 1 SR`** — statut, `00` = OK |

Référence :
```xml
<macro name="SETADDR"><command>SEND 01 F4</command></macro>
<command name="setaddr"><send>SYNC;SETADDR $addr $hexlen;SEND BYTES $unit;RECV 1 SR</send></command>
```

**Conséquence actuelle** : les hooks MQTT `openv/setTempWWsoll` et
`openv/setBetriebArtM1` n'ont jamais pu agir sur la chaudière.

**Test d'acceptation** : écrire la consigne ECS à sa valeur courante, vérifier
le retour `0x00`, puis relire `0x6300` et retrouver la valeur.

---

## Tâche 2 — Corriger la transformation de `setTempWWsoll`

`config-openv.yaml`, section `mqtt_hooks`.

Actuel : `transform: "multiply_10"` sur l'adresse `0x6300`.

Le XML communautaire dit :
```xml
<command name="setTempWWsoll" protocmd="setaddr">
  <addr>6300</addr>  <len>1</len>  <unit>ST</unit>
</command>
```

**Un seul octet, unité `ST` (`calc get="V" set="V"`, aucun facteur).** Une
consigne de 48 °C deviendrait 480 avec `multiply_10` — impossible dans un octet.

C'est précisément l'objet du dernier correctif amont, *« setTempWWsoll's length
is reportedly actually 1 »* (février 2023, après un aller-retour commit/revert).

**Correction** : supprimer `transform`, poser `len: 1`. Vérifier de la même
façon les bornes `min_value: 35` / `max_value: 65`.

---

## Tâche 3 — Piloter le protocole depuis le XML, pas depuis le code

**C'est le correctif structurel, et il aurait évité les tâches 1 et 2.**

Le XML communautaire décrit déjà la séquence complète (`SYNC`, `GETADDR`,
`SETADDR`, `RECV`). Le Go la réimplémente en dur, d'où la divergence.

`internal/xml/` parse déjà `<unit>` (`units.go`) et `<command>` (`vito.go`).
Il manque le bloc `<protocol name="KW2">` avec ses `<macros>`.

**À faire** : ajouter le parsing des macros, et construire les trames à partir
de là. Les constantes `0x04 / 0x05 / 0x01 / 0xF7 / 0xF4` disparaissent du code
et deviennent des données.

*Bénéfice attendu : la moindre correction amont du protocole est reprise en
relançant `make deps`, sans toucher au Go.*

---

## Tâche 4 — Borner `readNBytes` (robustesse)

Même fichier. Sur timeout, `port.Read` renvoie 0 octet **sans erreur**, et le
code fait `continue` — sans compteur ni échéance :

```go
if readBytes == 0 {
    continue // Timeout, try again   <-- boucle infinie
}
```

Si la chaudière ne répond pas, la goroutine tourne indéfiniment **en tenant le
mutex** : tout le daemon se fige. `KW2_MAX_RETRIES` est défini mais jamais
utilisé.

Second point, même fonction : l'assertion de type
`if setter, ok := sc.port.(interface{ SetReadTimeout(...) })` échoue
silencieusement si l'implémentation ne l'expose pas — et `Read` bloque alors
pour toujours. **Traiter `ok == false` comme une erreur.**

---

## Tâche 5 — Étendre la liste MQTT et poser les `state_class`

`config-openv.yaml` ne publie que **trois adresses**. C'est la raison pour
laquelle si peu de données remontaient.

```yaml
mqtt:
  addresses:
  - {address: "0x08A7", alias: getBrennerStunden1}   # heures brûleur (unité CS)
  - {address: "0x088A", alias: getBrennerStarts}     # démarrages (unité CO)
  - {address: "0x551E", alias: getBrennerStatus}     # brûleur 0/1 (RT)
  - {address: "0x2906", alias: getPumpeStatusM1}     # circulateur 0/1 (RT)
  - {address: "0x0800", alias: getTempAussen}
  - {address: "0x0802", alias: getTempKist}
  - {address: "0x0810", alias: getTempWWist}
  - {address: "0x0811", alias: getTempVLsoll}
  - {address: "0x2301", alias: getBetriebArtM1}      # mode (BA)
```

**Et surtout** : publier des messages de découverte Home Assistant **retenus**,
avec `state_class`. C'est ce qui a manqué la dernière fois — un an d'historique
s'est évaporé au bout de dix jours parce que HA n'archive en statistiques long
terme que les entités qui en déclarent un.

```json
{
  "name": "Heures brûleur",
  "unique_id": "vitola_burner_hours",
  "state_topic": "openv/getBrennerStunden1",
  "unit_of_measurement": "h",
  "state_class": "total_increasing",
  "device": { "identifiers": ["vitola200"], "name": "Vitola 200" }
}
```

`total_increasing` pour les compteurs, `measurement` pour les températures.

Table des modes (unité `BA`, adresse `0x2301`) :

| Code | Texte | Sens |
|---|---|---|
| `00` | `WW` | eau chaude seule |
| `01` | `RED` | chauffage réduit |
| `02` | `NORM` | chauffage normal |
| `03`/`04` | `H+WW` | chauffage + eau chaude |
| `05` | `ABSCHALT` | arrêt, hors gel |

---

## Tâche 6 — Supprimer la conversion en double

`internal/viessmann/value_decoder.go`, `DecodeCycleTime` : un
`if raw > 3600 { hours := raw/3600 }` codé en dur coexiste avec l'unité `CS`
du XML (`calc get="V/3600"`, entity `Stunden`).

Deux chemins pour la même conversion, donc deux résultats possibles. C'est la
source probable de l'ambiguïté sur une valeur de `15.1` observée en MQTT en
octobre 2025 — invraisemblable pour un cumul face à 196 980 démarrages.

**Laisser l'XML faire la conversion, supprimer le chemin en dur.**

---

## Tâche 7 — Décider du sort de `DP_Listen_2.zip`

`config-openv.yaml` référence
`../optolink-splitter/DP_Listen_2.zip:DP_Listen - Kopie/DP_V200KW2_6.txt`.

**Ce fichier n'existe pas en amont** : il n'est pas suivi par git dans
`philippoo66/optolink-splitter`, et n'est présent que sur la branche locale
`vs1test`. `scripts/bootstrap.sh` le récupère depuis la copie locale, ce qui
n'est pas reproductible sur une machine neuve.

**À trancher** : soit `vito.xml` suffit pour toutes les adresses utilisées —
et on supprime la dépendance — soit le fichier est nécessaire et il faut le
verser dans `data/` du dépôt (1,8 Mo).

---

## Tâche 8 — Réparer la suite de tests (préexistant)

Constaté au moment de l'import, **antérieur à toute modification** :

- `cmd/vcontrold-go/main_test.go:486` ne compile pas —
  `reader.deduplicateDataPoints undefined (cannot refer to unexported method)`.
  Le paquet entier échoue donc au `go test`.
- `TestParseAndValidateConfig`, `TestConfigValidatorWithRealData`,
  `TestClient_ClimateCommandHandlers`, `TestClient_ClimateModeHandlers` échouent.
- Plusieurs tests référencent `xml/vcontrold.xml` et `xml/vito.xml`, chemins
  **gitignorés** : sur un clone neuf ils ne trouveront rien.
  **Les faire pointer sur `third_party/vcontrold/xml/kw/`.**

`go build ./...` passe, seuls les tests sont en cause.

---

## Tâche 9 — Nettoyage

- `KW2_CHECKSUM_INIT = 0xF7` est mal nommé : c'est l'**opcode de lecture**, pas
  un checksum. Renommer en `KW2_CMD_READ`, ajouter `KW2_CMD_WRITE = 0xF4`.
  *Cette confusion est probablement à l'origine de la tâche 1.*
- `go.mod` déclare `github.com/yourusername/vcontrold-go` — placeholder.
- Les fichiers `test_*.go` et `debug-mqtt-hooks.go` sont à la racine avec des
  `func main`, hors de `cmd/`. À déplacer ou supprimer.

---

## Ordre recommandé

| | Tâche | Débloque |
|---|---|---|
| 1 | **5** — liste MQTT + `state_class` | **les heures de brûleur, l'objectif métier** |
| 2 | **4** — borner `readNBytes` | fiabilité d'une collecte longue |
| 3 | **1** — écriture `0xF4` | le pilotage depuis HA |
| 4 | **2** — `setTempWWsoll` | idem |
| 5 | **6** puis **9** | cohérence des conversions |
| — | **8** | suite de tests verte (préalable à toute CI) |
| 6 | **3** — protocole depuis le XML | évite que ça recommence |
| 7 | **7** — dépendance DP list | reproductibilité |

**La tâche 5 seule suffit à débloquer le besoin réel.** Les tâches 1 et 2
concernent le pilotage, qui est un confort. La tâche 3 est l'investissement de
fond.

---

## Vérification

```bash
make deps          # récupère les XML communautaires
make build test
make probe         # vérifie que l'Optolink est détecté sur la cible
make deploy && make run-remote
```

Puis, la mesure qui compte :

```bash
vclient -h localhost:3002 -c getBrennerStunden1
vclient -h localhost:3002 -c getBrennerStarts
```

Attendu pour les démarrages : une valeur **supérieure à 196 980** (relevé figé
en octobre 2025). L'écart donnera le rythme de cyclage sur dix mois.

Pour les heures : toute valeur cohérente avec ~197 000 démarrages, soit des
dizaines de milliers d'heures. **Si le résultat ressemble à `15.1`, la tâche 6
n'est pas faite** ou le compteur a été remis à zéro.
