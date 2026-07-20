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

    awk '
      # Remove document separator if present
      NR == 1 && $0 == "---" { next }

      # Prepend crd.enable check
      !guard { print "{{- if .Values.crd.enable }}"; guard = 1 }

      { print }

      # Insert crd.keep check and annotation after controller-gen version annotation
      /controller-gen\.kubebuilder\.io\/version:/ {
        print "    {{- if and .Values.crd.keep .Values.template.helmLabels }}"
        print "    helm.sh/resource-policy: keep"
        print "    {{- end }}"
      }

      # Append end statement
      END { print "{{- end }}" }
    ' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
  done
fi
