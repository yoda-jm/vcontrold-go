# Conventions de travail

Ce fichier est chargé automatiquement par Claude Code. Il décrit **comment on
travaille sur ce dépôt** ; le *quoi faire* est dans [PLAN.md](PLAN.md).

## Le montage physique

Deux machines, et c'est structurant :

| | Rôle |
|---|---|
| **Poste de développement** | on écrit, on compile, on teste unitairement |
| **Machine cible** | l'Optolink USB y est branché ; c'est la seule qui parle à la chaudière |

**On ne compile jamais sur la cible.** On construit ici, on envoie, on exécute
là-bas :

```bash
make build          # compilation croisee linux/amd64
make deploy         # rsync binaire + config + XML
make run-remote     # execution sur la cible
make probe          # verifie que l'adaptateur est detecte
```

Les paramètres viennent de `.env` (voir `.env.example`), surchargeables :
`make deploy TARGET_HOST=user@host`.

## Ce qui n'est pas dans le dépôt

**Aucun secret, aucune adresse interne.** Trois fichiers sont gitignorés et
doivent être créés à partir de leur modèle :

| Fichier réel | Modèle |
|---|---|
| `.env` | `.env.example` |
| `config-openv.yaml` | `config.example.yaml` |
| `third_party/` | `make deps` |

Avant tout commit, vérifier qu'aucun identifiant ni adresse RFC1918 ne part :

```bash
git diff --cached | grep -niE "password|token|192\.168\.|10\.[0-9]+\." && echo "STOP"
```

## Les XML communautaires font autorité

`third_party/vcontrold/xml/kw/` contient `vcontrold.xml` (unités, macros de
protocole) et `vito.xml` (adresses, longueurs). **Ces fichiers portent la
vérité du protocole.**

Règle : **si une valeur existe dans le XML, ne pas la coder en dur en Go.**
C'est exactement l'origine du bug d'écriture — l'opcode `0xF4` était dans le
XML depuis le début, mais le Go avait sa propre constante.

Ils sont épinglés à une révision précise dans `scripts/bootstrap.sh`. Les
remonter est un geste volontaire, jamais un effet de bord.

## Vérifier sur le matériel, pas seulement en test

Les tests unitaires ne prouvent rien sur le protocole série. Toute modification
touchant `internal/viessmann/serial_communicator.go` doit être validée par une
lecture réelle :

```bash
vclient -h localhost:3002 -c getBrennerStunden1
vclient -h localhost:3002 -c getBrennerStarts
```

**Critère de cohérence** : les démarrages doivent dépasser `196980` (relevé du
7 octobre 2025). Les heures doivent être cohérentes avec ce nombre de
démarrages, soit des dizaines de milliers — une valeur de l'ordre de `15`
signale une conversion fautive.

## Prudence sur l'écriture

Les commandes `set*` agissent sur une chaudière en service, l'hiver, dans une
maison habitée. **Ne jamais écrire à l'aveugle.**

Protocole : lire la valeur, la réécrire à l'identique, vérifier le retour `00`,
relire. Seulement ensuite, modifier.

## Style

- Messages de commit en français, à l'impératif, et **expliquant le pourquoi**
  quand le quoi ne suffit pas.
- Pas de binaire compilé dans le dépôt (`make clean` avant de committer).
- Les fichiers de travail écartés sont dans `.archive-local/`, gitignoré :
  rien n'a été supprimé, mais rien n'ira sur GitHub.
