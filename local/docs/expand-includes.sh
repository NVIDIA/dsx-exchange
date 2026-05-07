#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -e

# Expand ::include{file=...} directives in markdown files
# Processes .tmpl.md files and outputs .md files with expanded includes

expand_file() {
  local input_file="$1"
  local output_file="${input_file%.tmpl.md}.md"
  local file_dir
  file_dir="$(dirname "${input_file}")"

  local temp_file
  temp_file="$(mktemp)"

  while IFS= read -r line || [[ -n "${line}" ]]; do
    # Process include directives
    if [[ "${line}" =~ ::include\{file=([^}]+)\} ]]; then
      local include_file="${BASH_REMATCH[1]}"
      local include_path="${file_dir}/${include_file}"

      if [[ ! -f "${include_path}" ]]; then
        echo "Warning: Include file not found: ${include_path}" >&2
        echo "${line}" >> "${temp_file}"
        continue
      fi

      cat "${include_path}" >> "${temp_file}"
    else
      echo "${line}" >> "${temp_file}"
    fi
  done < "${input_file}"

  mv "${temp_file}" "${output_file}"
  echo "Expanded: ${input_file} -> ${output_file}"
}

# Main execution
main() {
  local docs_dir
  docs_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

  # Find all .tmpl.md files
  while IFS= read -r -d '' tmpl_file; do
    expand_file "${tmpl_file}"
  done < <(find "${docs_dir}" -name "*.tmpl.md" -print0)
}

# Run if executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
