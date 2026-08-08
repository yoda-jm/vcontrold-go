#!/usr/bin/env bash
# Récupère les fichiers de configuration issus des projets communautaires.
# Idempotent : relancer met simplement à jour.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPS="$ROOT/third_party"

# Révisions épinglées : à remonter volontairement, jamais automatiquement.
VCONTROLD_REPO="https://github.com/openv/vcontrold.git"
VCONTROLD_REF="8ca4797"          # HEAD upstream au 2024-09-04

SPLITTER_REPO="https://github.com/philippoo66/optolink-splitter.git"
SPLITTER_REF="c1ee204"   # main upstream ; ab6c010 est une branche locale absente de l'amont

fetch() {
  local name="$1"
  local repo="$2"
  local ref="$3"
  local dir="$DEPS/$name"
  if [ -d "$dir/.git" ]; then
    echo "==> $name : mise à jour"
    git -C "$dir" fetch --quiet origin
  else
    echo "==> $name : clonage"
    git clone --quiet "$repo" "$dir"
  fi
  git -C "$dir" checkout --quiet "$ref"
  echo "    $name @ $(git -C "$dir" log -1 --format='%h %cd' --date=short)"
}

mkdir -p "$DEPS"
fetch vcontrold        "$VCONTROLD_REPO" "$VCONTROLD_REF"
fetch optolink-splitter "$SPLITTER_REPO"  "$SPLITTER_REF"

# --- vérification des fichiers réellement utilisés ---------------------------
need=(
  "$DEPS/vcontrold/xml/kw/vcontrold.xml"
  "$DEPS/vcontrold/xml/kw/vito.xml"
)
missing=0
for f in "${need[@]}"; do
  if [ -f "$f" ]; then
    echo "    ok  ${f#$ROOT/}"
  else
    echo "    MANQUANT  ${f#$ROOT/}" >&2
    missing=1
  fi
done

# La liste de datapoints DP_Listen_2.zip n'est PAS versionnee en amont :
# elle n'existe sur aucune branche publique de optolink-splitter.
# Elle est donc consideree comme optionnelle -- vito.xml porte deja les adresses.
# Voir PLAN.md, tache 7.
DP_ZIP="$DEPS/optolink-splitter/DP_Listen_2.zip"
if [ -f "$DP_ZIP" ]; then
  echo "    ok  ${DP_ZIP#$ROOT/}"
else
  echo "    info : DP_Listen_2.zip absent de l'amont -- optionnel, voir PLAN.md tache 7."
fi

[ "$missing" -eq 0 ] || { echo "Bootstrap incomplet." >&2; exit 1; }

cat <<EOF

Bootstrap terminé. Chemins à utiliser dans la configuration :

  vcontrol_xml: third_party/vcontrold/xml/kw/vcontrold.xml
  vito_xml:     third_party/vcontrold/xml/kw/vito.xml

EOF
