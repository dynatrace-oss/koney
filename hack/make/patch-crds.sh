#!/bin/bash
set -e

CRD_DIR="$1"

if [ -z "$CRD_DIR" ]; then
  echo "Error: CRD_DIR argument is missing"
  exit 1
fi

if [ -d "$CRD_DIR" ]; then
  for file in "$CRD_DIR"/*.yaml; do
    echo "Patching $file with Helm templates ..."

    # Remove document separator if present
    sed -i '1{/^---$/d}' "$file"

    # Prepend 'crd.enable' check
    sed -i '1i{{- if .Values.crd.enable }}' "$file"

    # Append end statement
    echo "{{- end }}" >> "$file"

    # Insert 'crd.keep' check and annotation
    sed -i '/controller-gen.kubebuilder.io\/version:/a \    {{- if and .Values.crd.keep .Values.template.helmLabels }}\n    helm.sh/resource-policy: keep\n    {{- end }}' "$file"
  done
fi
