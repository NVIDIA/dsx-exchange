#!/usr/bin/env python3
"""Generate Fern MDX pages from AsyncAPI 3.1.0 YAML specs.

Reads the four AsyncAPI specs under schemas/asyncapi/ and writes native MDX
pages to docs/, replacing the previous iframe stubs.  Only stdlib is used
(a minimal YAML parser is embedded so PyYAML is not required).

Usage:
    python3 scripts/generate_asyncapi_docs.py          # regenerate all
    python3 scripts/generate_asyncapi_docs.py --spec bms
    python3 scripts/generate_asyncapi_docs.py --dry-run
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import textwrap
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"

SPECS = {
    "bms": ROOT / "schemas" / "asyncapi" / "bms" / "bms.yaml",
    "power-management": ROOT / "schemas" / "asyncapi" / "power-management" / "power-management.yaml",
    "nico": ROOT / "schemas" / "asyncapi" / "nico" / "nico.yaml",
    "spiffe-exchange": ROOT / "schemas" / "asyncapi" / "spiffe-exchange" / "pub-keysets.yaml",
}

# ---------------------------------------------------------------------------
# Minimal YAML parser (subset: mappings, sequences, scalars, block strings)
# ---------------------------------------------------------------------------

class _YamlParser:
    """Parse a YAML document using only stdlib.

    Supports the subset used by AsyncAPI specs: block mappings, block
    sequences, flow sequences, quoted/unquoted scalars, | and > block
    scalars, comments, and anchors/aliases are NOT supported (none of
    the specs use them).
    """

    def __init__(self, text: str):
        self.lines = text.splitlines()
        self.pos = 0

    def parse(self) -> Any:
        self._skip_blanks_and_comments()
        if self.pos >= len(self.lines):
            return None
        return self._parse_value(0)

    # -- helpers --

    def _indent(self, line: str) -> int:
        return len(line) - len(line.lstrip())

    def _skip_blanks_and_comments(self):
        while self.pos < len(self.lines):
            stripped = self.lines[self.pos].strip()
            if stripped == "" or stripped.startswith("#"):
                self.pos += 1
            else:
                break

    def _peek_line(self) -> str | None:
        self._skip_blanks_and_comments()
        if self.pos >= len(self.lines):
            return None
        return self.lines[self.pos]

    @staticmethod
    def _strip_comment(text: str) -> str:
        """Remove trailing YAML comment, respecting quotes."""
        in_single = False
        in_double = False
        for i, ch in enumerate(text):
            if ch == "'" and not in_double:
                in_single = not in_single
            elif ch == '"' and not in_single:
                in_double = not in_double
            elif ch == "#" and not in_single and not in_double:
                if i == 0 or text[i - 1] == " ":
                    return text[:i].strip()
        return text.strip()

    def _is_mapping_key(self, line: str) -> bool:
        """Check if a line looks like a YAML mapping key (has unquoted colon)."""
        stripped = line.strip()
        if stripped.startswith("-") or stripped.startswith("{") or stripped.startswith("["):
            return False
        in_single = False
        in_double = False
        for i, ch in enumerate(stripped):
            if ch == "'" and not in_double:
                in_single = not in_single
            elif ch == '"' and not in_single:
                in_double = not in_double
            elif ch == ":" and not in_single and not in_double:
                if i + 1 == len(stripped) or stripped[i + 1] == " ":
                    return True
        return False

    def _parse_value(self, min_indent: int) -> Any:
        line = self._peek_line()
        if line is None:
            return None
        indent = self._indent(line)
        stripped = line.strip()

        if stripped.startswith("- ") or stripped == "-":
            return self._parse_sequence(indent)
        if self._is_mapping_key(stripped):
            return self._parse_mapping(indent)
        if stripped.startswith("{"):
            return self._parse_flow_mapping(line, indent)
        if stripped.startswith("["):
            return self._parse_flow_sequence(line, indent)
        # bare scalar — may span multiple lines (plain scalar continuation)
        return self._parse_plain_scalar(indent)

    def _parse_plain_scalar(self, base_indent: int) -> Any:
        """Parse a plain (unquoted) scalar that may span multiple lines."""
        parts = []
        while self.pos < len(self.lines):
            line = self.lines[self.pos]
            stripped = line.strip()
            if stripped == "" or stripped.startswith("#"):
                break
            ind = self._indent(line)
            if ind < base_indent:
                break
            if parts and ind == base_indent and self._is_mapping_key(stripped):
                break
            if parts and stripped.startswith("- "):
                break
            parts.append(stripped)
            self.pos += 1
        text = " ".join(parts)
        return self._scalar(text)

    def _parse_mapping(self, base_indent: int) -> dict:
        result = {}
        while True:
            line = self._peek_line()
            if line is None:
                break
            indent = self._indent(line)
            if indent < base_indent:
                break
            if indent > base_indent:
                break
            stripped = line.strip()
            if stripped.startswith("- "):
                break

            # extract key: value
            m = re.match(r'^(\s*)([\w$\-./]+|"[^"]*"|\'[^\']*\')\s*:\s*(.*)', line)
            if not m:
                self.pos += 1
                continue
            key = m.group(2).strip("'\"")
            rest = m.group(3)
            rest_stripped = self._strip_comment(rest) if rest else ""
            self.pos += 1

            if rest_stripped in ("|", ">", "|+", ">+", "|-", ">-"):
                result[key] = self._parse_block_scalar(rest_stripped, base_indent)
            elif rest_stripped == "":
                # value on next lines
                next_line = self._peek_line()
                if next_line is not None and self._indent(next_line) > base_indent:
                    result[key] = self._parse_value(self._indent(next_line))
                else:
                    result[key] = None
            elif rest_stripped.startswith("["):
                result[key] = self._parse_inline_flow_seq(rest_stripped)
            elif rest_stripped.startswith("{"):
                result[key] = self._parse_inline_flow_map(rest_stripped)
            else:
                result[key] = self._scalar(rest_stripped)
        return result

    def _parse_sequence(self, base_indent: int) -> list:
        result = []
        while True:
            line = self._peek_line()
            if line is None:
                break
            indent = self._indent(line)
            if indent < base_indent:
                break
            if indent > base_indent:
                break
            stripped = line.strip()
            if not stripped.startswith("-"):
                break

            after_dash = stripped[1:].strip() if len(stripped) > 1 else ""
            self.pos += 1

            if after_dash == "":
                next_line = self._peek_line()
                if next_line is not None and self._indent(next_line) > base_indent:
                    result.append(self._parse_value(self._indent(next_line)))
                else:
                    result.append(None)
            elif ":" in after_dash and not after_dash.startswith("{") and not after_dash.startswith("[") and not after_dash.startswith("'") and not after_dash.startswith('"'):
                # inline mapping after dash: "- key: value"
                inner = self._parse_dash_inline_mapping(after_dash, base_indent + 2)
                result.append(inner)
            elif after_dash.startswith("["):
                result.append(self._parse_inline_flow_seq(after_dash))
            elif after_dash.startswith("{"):
                result.append(self._parse_inline_flow_map(after_dash))
            else:
                result.append(self._scalar(after_dash))
        return result

    def _parse_dash_inline_mapping(self, first_kv: str, continuation_indent: int) -> dict:
        """Parse '- key: val' followed by continuation lines at deeper indent."""
        result = {}
        m = re.match(r'([\w$\-./]+|"[^"]*"|\'[^\']*\')\s*:\s*(.*)', first_kv)
        if m:
            key = m.group(1).strip("'\"")
            rest = self._strip_comment(m.group(2))
            if rest:
                result[key] = self._scalar(rest)
            else:
                next_line = self._peek_line()
                if next_line is not None and self._indent(next_line) >= continuation_indent:
                    result[key] = self._parse_value(self._indent(next_line))
                else:
                    result[key] = None

        # continue reading mapping entries at continuation_indent
        while True:
            line = self._peek_line()
            if line is None:
                break
            indent = self._indent(line)
            if indent < continuation_indent:
                break
            stripped = line.strip()
            m2 = re.match(r'^(\s*)([\w$\-./]+|"[^"]*"|\'[^\']*\')\s*:\s*(.*)', line)
            if not m2 or indent != continuation_indent:
                break
            key = m2.group(2).strip("'\"")
            rest = self._strip_comment(m2.group(3))
            self.pos += 1
            if rest in ("|", ">", "|+", ">+", "|-", ">-"):
                result[key] = self._parse_block_scalar(rest, continuation_indent)
            elif rest == "":
                next_line = self._peek_line()
                if next_line is not None and self._indent(next_line) > continuation_indent:
                    result[key] = self._parse_value(self._indent(next_line))
                else:
                    result[key] = None
            elif rest.startswith("["):
                result[key] = self._parse_inline_flow_seq(rest)
            elif rest.startswith("{"):
                result[key] = self._parse_inline_flow_map(rest)
            else:
                result[key] = self._scalar(rest)
        return result

    def _parse_block_scalar(self, indicator: str, base_indent: int) -> str:
        """Parse | or > block scalar."""
        folded = indicator.startswith(">")
        lines: list[str] = []
        block_indent: int | None = None
        while self.pos < len(self.lines):
            raw = self.lines[self.pos]
            # Inside block scalars, '#' starts literal text (e.g. Markdown headings),
            # not a YAML comment. Preserve such lines as content.
            if raw.strip() == "":
                if block_indent is not None:
                    lines.append("")
                    self.pos += 1
                    continue
                lines.append("")
                self.pos += 1
                continue
            ind = self._indent(raw)
            if ind <= base_indent:
                break
            if block_indent is None:
                block_indent = ind
            if ind < block_indent:
                break
            lines.append(raw[block_indent:])
            self.pos += 1

        # trim trailing blank lines unless + chomp
        if not indicator.endswith("+"):
            while lines and lines[-1] == "":
                lines.pop()

        if folded:
            # fold: join lines with spaces, preserve blank-line paragraph breaks
            paragraphs = []
            current: list[str] = []
            for ln in lines:
                if ln == "":
                    if current:
                        paragraphs.append(" ".join(current))
                        current = []
                    paragraphs.append("")
                else:
                    current.append(ln)
            if current:
                paragraphs.append(" ".join(current))
            return "\n".join(paragraphs) + "\n" if paragraphs else ""
        else:
            return "\n".join(lines) + "\n" if lines else ""

    def _parse_flow_mapping(self, line: str, indent: int) -> dict:
        self.pos += 1
        return self._parse_inline_flow_map(line.strip())

    def _parse_flow_sequence(self, line: str, indent: int) -> list:
        self.pos += 1
        return self._parse_inline_flow_seq(line.strip())

    def _parse_inline_flow_seq(self, text: str) -> list:
        text = text.strip()
        if text.startswith("[") and text.endswith("]"):
            inner = text[1:-1].strip()
            if not inner:
                return []
            items = self._split_flow(inner)
            return [self._scalar(i.strip()) for i in items]
        return [self._scalar(text)]

    def _parse_inline_flow_map(self, text: str) -> dict:
        text = text.strip()
        if text.startswith("{") and text.endswith("}"):
            inner = text[1:-1].strip()
            if not inner:
                return {}
            result = {}
            for pair in self._split_flow(inner):
                if ":" in pair:
                    k, v = pair.split(":", 1)
                    result[k.strip().strip("'\"")] = self._scalar(v.strip())
            return result
        return {}

    def _split_flow(self, text: str) -> list[str]:
        """Split comma-separated flow items, respecting brackets."""
        items = []
        depth = 0
        current = []
        for ch in text:
            if ch in ("{", "["):
                depth += 1
                current.append(ch)
            elif ch in ("}", "]"):
                depth -= 1
                current.append(ch)
            elif ch == "," and depth == 0:
                items.append("".join(current))
                current = []
            else:
                current.append(ch)
        if current:
            items.append("".join(current))
        return items

    def _scalar(self, text: str) -> Any:
        text = _YamlParser._strip_comment(text)
        if not text:
            return None
        # quoted
        if (text.startswith("'") and text.endswith("'")) or (text.startswith('"') and text.endswith('"')):
            return text[1:-1]
        # special values
        low = text.lower()
        if low in ("null", "~"):
            return None
        if low == "true":
            return True
        if low == "false":
            return False
        # number
        try:
            if "." in text:
                return float(text)
            return int(text)
        except ValueError:
            pass
        return text


def load_yaml(path: Path) -> dict:
    text = path.read_text()
    return _YamlParser(text).parse()


# ---------------------------------------------------------------------------
# Ref resolver
# ---------------------------------------------------------------------------

def resolve_ref(spec: dict, ref: str) -> dict:
    """Resolve a $ref like '#/components/schemas/Foo'."""
    if not ref.startswith("#/"):
        return {}
    parts = ref[2:].split("/")
    node = spec
    for p in parts:
        if isinstance(node, dict):
            node = node.get(p, {})
        else:
            return {}
    return node if isinstance(node, dict) else {}


def resolve_schema(spec: dict, schema: dict | None, depth: int = 0) -> dict:
    """Resolve $ref and allOf, returning a normalized schema dict."""
    if schema is None:
        return {}
    if depth > 10:
        return schema
    if "$ref" in schema:
        resolved = resolve_ref(spec, schema["$ref"])
        return resolve_schema(spec, resolved, depth + 1)
    if "allOf" in schema:
        merged: dict = {}
        merged_props: dict = {}
        merged_required: list = []
        for item in schema["allOf"]:
            sub = resolve_schema(spec, item, depth + 1)
            for k, v in sub.items():
                if k == "properties":
                    merged_props.update(v)
                elif k == "required":
                    merged_required.extend(v)
                else:
                    merged[k] = v
        if merged_props:
            merged["properties"] = merged_props
        if merged_required:
            merged["required"] = list(dict.fromkeys(merged_required))
        return merged
    return schema


# ---------------------------------------------------------------------------
# Schema rendering
# ---------------------------------------------------------------------------

def _schema_anchor(name: str) -> str:
    """Build a predictable markdown anchor slug from a schema name."""
    slug = re.sub(r"[^a-z0-9\\s-]", "", name.lower())
    slug = re.sub(r"\\s+", "-", slug).strip("-")
    return slug or name.lower()


def _type_str(schema: dict, spec: dict, schema_doc: str | None = None) -> str:
    """Human-readable type string."""
    if "$ref" in schema:
        ref = schema["$ref"]
        name = ref.split("/")[-1]
        if ref.startswith("#/components/schemas/"):
            anchor = _schema_anchor(name)
            if schema_doc:
                return f"[{name}]({schema_doc}#{anchor})"
            return f"[{name}](#{anchor})"
        return name

    t = schema.get("type", "")
    if isinstance(t, list):
        return " or ".join(str(x) for x in t)

    if "oneOf" in schema:
        parts = []
        for item in schema["oneOf"]:
            parts.append(_type_str(item, spec, schema_doc=schema_doc))
        return " or ".join(parts)

    if "allOf" in schema:
        return "object"

    if t == "array":
        items = schema.get("items", {})
        inner = _type_str(items, spec, schema_doc=schema_doc)
        return f"array\\<{inner}\\>"
    if t == "object" and "additionalProperties" in schema:
        ap = schema["additionalProperties"]
        inner = _type_str(ap, spec, schema_doc=schema_doc) if isinstance(ap, dict) else "any"
        return f"map\\<string, {inner}\\>"
    if t == "string" and schema.get("format"):
        return f"string ({schema['format']})"
    if t == "number" and schema.get("format"):
        return f"number ({schema['format']})"
    if t == "integer" and schema.get("format"):
        return f"integer ({schema['format']})"
    return t or "any"


def render_property_table(
    schema: dict | None,
    spec: dict,
    depth: int = 0,
    max_depth: int = 2,
    prefix: str = "",
    schema_doc: str | None = None,
) -> str:
    """Render a schema as an MDX property table."""
    if schema is None:
        return ""
    schema = resolve_schema(spec, schema)

    if "oneOf" in schema and depth < max_depth:
        return _render_oneof(schema, spec, depth, max_depth, schema_doc=schema_doc)

    if "anyOf" in schema and depth < max_depth:
        return _render_oneof(schema, spec, depth, max_depth, key="anyOf", schema_doc=schema_doc)

    props = schema.get("properties", {})
    required = set(schema.get("required", []))
    if not props:
        enum = schema.get("enum")
        if enum:
            return "**Allowed values:** " + ", ".join(f"`{v}`" for v in enum) + "\n"
        desc = schema.get("description", "")
        ts = _type_str(schema, spec, schema_doc=schema_doc)
        if desc or ts:
            return f"**Type:** {ts}\n\n{desc}\n"
        return ""

    rows = []
    _collect_rows(props, required, spec, depth, max_depth, prefix, rows, schema_doc=schema_doc)

    if not rows:
        return ""

    lines = ["| Name | Type | Required | Description |",
             "|------|------|----------|-------------|"]
    for name, typ, req, desc in rows:
        desc_clean = desc.replace("\n", " ").replace("|", "\\|").strip()
        if len(desc_clean) > 200:
            desc_clean = desc_clean[:197] + "..."
        lines.append(f"| `{name}` | {typ} | {req} | {desc_clean} |")

    return "\n".join(lines) + "\n"


def _collect_rows(
    props: dict,
    required: set,
    spec: dict,
    depth: int,
    max_depth: int,
    prefix: str,
    rows: list,
    schema_doc: str | None = None,
):
    for name, prop_schema in props.items():
        full_name = f"{prefix}{name}"
        resolved = resolve_schema(spec, prop_schema)
        typ = _type_str(prop_schema, spec, schema_doc=schema_doc) if "$ref" in prop_schema else _type_str(resolved, spec, schema_doc=schema_doc)
        req = "Yes" if name in required else "No"
        desc = resolved.get("description", "") or ""
        enum = resolved.get("enum")
        if enum:
            vals = ", ".join(f"`{v}`" for v in enum[:8])
            if len(enum) > 8:
                vals += f", ... ({len(enum)} total)"
            desc = desc.rstrip()
            if desc:
                desc += " "
            desc += f"Values: {vals}"
        const = resolved.get("const")
        if const is not None:
            desc += f" Must be `{const}`."

        rows.append((full_name, typ, req, desc))

        # expand nested object properties with dot notation
        if depth < max_depth and resolved.get("type") == "object" and resolved.get("properties"):
            inner_required = set(resolved.get("required", []))
            _collect_rows(
                resolved["properties"], inner_required, spec,
                depth + 1, max_depth, f"{full_name}.", rows, schema_doc=schema_doc,
            )


def _render_oneof(
    schema: dict,
    spec: dict,
    depth: int,
    max_depth: int,
    key: str = "oneOf",
    schema_doc: str | None = None,
) -> str:
    variants = schema.get(key, [])
    desc = schema.get("description", "")
    parts = []
    if desc:
        parts.append(desc.strip())
        parts.append("")

    for i, variant in enumerate(variants):
        resolved = resolve_schema(spec, variant)
        variant_desc = resolved.get("description", "")

        # try to find discriminator from enum in 'state' or 'type' field
        label = None
        for disc_field in ("state", "type"):
            disc_props = resolved.get("properties", {}).get(disc_field, {})
            disc_resolved = resolve_schema(spec, disc_props)
            if disc_resolved.get("enum") and len(disc_resolved["enum"]) == 1:
                label = str(disc_resolved["enum"][0])
                break
        label_from_desc = False
        if not label and variant_desc:
            # try extracting a bold/italic label from the description
            m2 = re.match(r'^[*_]{1,2}(.+?)[*_]{1,2}', variant_desc.strip())
            if m2:
                label = m2.group(1)
                label_from_desc = True
            else:
                first_line = variant_desc.strip().split("\n")[0].strip()
                label = first_line if len(first_line) <= 80 else first_line[:77] + "..."
                label_from_desc = True
        if not label:
            label = f"Variant {i + 1}"

        parts.append(f"### {label}")
        parts.append("")
        if variant_desc:
            body = variant_desc.strip()
            if label_from_desc:
                # strip the opening sentence that was used as the heading
                # find the first paragraph break or sentence end
                m3 = re.match(r'^[*_]{0,2}[^*_]*?[*_]{0,2}:?\s*.*?\.\s*\n', body, re.DOTALL)
                if m3:
                    body = body[m3.end():].strip()
                else:
                    # fall back to stripping lines until we hit a blank or list
                    body_lines = body.split("\n")
                    skip = 1
                    while skip < len(body_lines):
                        ln = body_lines[skip].strip()
                        if ln == "" or ln.startswith("-") or ln.startswith("|") or ln.startswith("#"):
                            break
                        skip += 1
                    body = "\n".join(body_lines[skip:]).strip()
            if body:
                parts.append(body)
                parts.append("")

        table = render_property_table(resolved, spec, depth + 1, max_depth, schema_doc=schema_doc)
        if table:
            parts.append(table)
        parts.append("")

    return "\n".join(parts)


# ---------------------------------------------------------------------------
# Example generation
# ---------------------------------------------------------------------------

def generate_example(schema: dict | None, spec: dict, depth: int = 0, max_depth: int = 3) -> Any:
    if schema is None or depth > max_depth:
        return None
    schema = resolve_schema(spec, schema)

    if "example" in schema:
        return schema["example"]
    if "examples" in schema and schema["examples"]:
        ex = schema["examples"]
        return ex[0] if isinstance(ex, list) else ex

    t = schema.get("type", "")
    if isinstance(t, list):
        t = t[0] if t else "string"

    if "oneOf" in schema:
        return generate_example(schema["oneOf"][0], spec, depth, max_depth)

    if t == "object":
        props = schema.get("properties", {})
        if not props:
            ap = schema.get("additionalProperties")
            if ap:
                inner = generate_example(ap if isinstance(ap, dict) else {}, spec, depth + 1, max_depth)
                return {"key": inner}
            return {}
        obj = {}
        for name, prop in props.items():
            obj[name] = generate_example(prop, spec, depth + 1, max_depth)
        return obj

    if t == "array":
        items = schema.get("items", {})
        inner = generate_example(items, spec, depth + 1, max_depth)
        return [inner] if inner is not None else []

    enum = schema.get("enum")
    if enum:
        return enum[0]

    const = schema.get("const")
    if const is not None:
        return const

    fmt = schema.get("format", "")
    if t == "string":
        if fmt == "date-time":
            return "2026-05-14T12:00:00Z"
        if fmt == "uuid":
            return "550e8400-e29b-41d4-a716-446655440000"
        return "string"
    if t in ("number", "integer"):
        mn = schema.get("minimum")
        if mn is not None:
            return mn
        return 0
    if t == "boolean":
        default = schema.get("default")
        return default if default is not None else False

    return None


# ---------------------------------------------------------------------------
# MDX escaping
# ---------------------------------------------------------------------------

def escape_mdx(text: str) -> str:
    """Escape { } and bare < > in MDX prose, preserving code blocks/spans."""
    lines = text.split("\n")
    out: list[str] = []
    in_code_block = False
    for line in lines:
        if line.strip().startswith("```"):
            in_code_block = not in_code_block
            out.append(line)
            continue
        if in_code_block:
            out.append(line)
            continue
        out.append(_escape_line(line))
    return "\n".join(out)


def collapse_blank_lines(text: str) -> str:
    """Collapse consecutive blank lines in Markdown content."""
    lines = text.split("\n")
    out: list[str] = []
    prev_blank = False

    for line in lines:
        is_blank = line.strip() == ""
        if is_blank:
            if prev_blank:
                continue
            prev_blank = True
            out.append("")
            continue

        prev_blank = False
        out.append(line)

    return "\n".join(out)


def _escape_line(line: str) -> str:
    """Escape { } outside backtick spans, and bare <word> as &lt;word&gt;."""
    result = []
    i = 0
    while i < len(line):
        if line[i] == "`":
            # find closing backtick
            j = line.index("`", i + 1) if "`" in line[i + 1:] else -1
            if j == -1:
                result.append(line[i:])
                break
            result.append(line[i:j + 1])
            i = j + 1
        elif line[i] == "{":
            result.append("\\{")
            i += 1
        elif line[i] == "}":
            result.append("\\}")
            i += 1
        elif line[i] == "<":
            # preserve known MDX/HTML tags
            rest = line[i:]
            if re.match(r"<(Accordion|/Accordion|details|/details|summary|/summary|iframe|Note|Warning|Tip|/Note|/Warning|/Tip|br\s*/?)[\s>]", rest):
                result.append(line[i])
                i += 1
            elif re.match(r"<[a-zA-Z]", rest) and ">" in rest:
                result.append("&lt;")
                i += 1
            else:
                result.append(line[i])
                i += 1
        else:
            result.append(line[i])
            i += 1
    return "".join(result)


# ---------------------------------------------------------------------------
# Page generators
# ---------------------------------------------------------------------------

def gen_overview(spec: dict, title_override: str | None = None, raw_yaml: str | None = None) -> str:
    info = spec.get("info", {})
    title = title_override or info.get("title", "Schema")
    version = info.get("version", "")
    desc = info.get("description", "")
    default_ct = spec.get("defaultContentType", "")

    lines = [f"# {title} {version}", ""]
    if desc:
        lines.append(desc.strip())
        lines.append("")
    if default_ct:
        lines.append(f"**Default Content Type:** `{default_ct}`")
        lines.append("")
    if raw_yaml:
        lines.append("## Raw AsyncAPI Spec")
        lines.append("")
        lines.append("<Accordion title=\"View / copy the raw AsyncAPI YAML\">")
        lines.append("")
        lines.append("````yaml")
        lines.append(raw_yaml.strip())
        lines.append("````")
        lines.append("")
        lines.append("</Accordion>")
        lines.append("")
    return "\n".join(lines) + "\n"


def gen_servers(spec: dict) -> str:
    servers = spec.get("servers", {})
    if not servers:
        return "# Servers\n\nNo servers defined in this specification.\n"
    lines = ["# Servers", ""]
    for name, server in servers.items():
        lines.append(f"## {name}")
        lines.append("")
        lines.append("| Field | Value |")
        lines.append("|-------|-------|")
        lines.append(f"| Host | `{server.get('host', '')}` |")
        lines.append(f"| Protocol | `{server.get('protocol', '')}` |")
        desc = server.get("description", "")
        if desc:
            lines.append(f"| Description | {desc.strip()} |")
        lines.append("")
    return "\n".join(lines) + "\n"


def gen_operation(spec: dict, op_name: str, page_title: str, schema_doc: str | None = None) -> str:
    ops = spec.get("operations", {})
    op = ops.get(op_name, {})
    if not op:
        return f"# {page_title}\n\nOperation `{op_name}` not found.\n"

    action = op.get("action", "send")
    direction = "Publish (send)" if action == "send" else "Subscribe (receive)"
    badge = "📤" if action == "send" else "📥"
    desc = op.get("description", op.get("summary", ""))

    # resolve channel
    channel_ref = op.get("channel", {})
    channel = resolve_schema(spec, channel_ref) if "$ref" in channel_ref else channel_ref
    address = channel.get("address", "")
    chan_desc = channel.get("description", "")
    params = channel.get("parameters", {})
    bindings = channel.get("bindings", {})

    lines = [f"# {page_title}", ""]
    if desc:
        lines.append(desc.strip())
        lines.append("")

    lines.append(f"**Direction:** {direction}")
    lines.append("")

    # Channel
    lines.append("## Channel")
    lines.append("")
    lines.append(f"```text")
    lines.append(address)
    lines.append("```")
    lines.append("")
    if chan_desc:
        lines.append(chan_desc.strip())
        lines.append("")

    # MQTT bindings
    mqtt_bind = bindings.get("mqtt", {})
    if mqtt_bind:
        qos = mqtt_bind.get("qos")
        if qos is not None:
            lines.append(f"**MQTT QoS:** {qos}")
            lines.append("")

    # Parameters
    if params:
        lines.append("### Parameters")
        lines.append("")
        lines.append("| Parameter | Description |")
        lines.append("|-----------|-------------|")
        for pname, pval in params.items():
            pdesc = ""
            if isinstance(pval, dict):
                pval_resolved = resolve_schema(spec, pval)
                pdesc = pval_resolved.get("description", "")
                enum = pval_resolved.get("enum")
                if enum:
                    vals = ", ".join(f"`{v}`" for v in enum[:6])
                    if len(enum) > 6:
                        vals += f" ... ({len(enum)} total)"
                    pdesc = pdesc.rstrip() + " " if pdesc else ""
                    pdesc += f"Values: {vals}"
            elif isinstance(pval, str):
                pdesc = pval
            lines.append(f"| `{pname}` | {pdesc.replace(chr(10), ' ').strip()} |")
        lines.append("")

    # Message payload — follow $ref chains (channel msg -> component msg)
    msg_refs = op.get("messages", [])
    messages = []
    for mref in msg_refs:
        if isinstance(mref, dict) and "$ref" in mref:
            resolved_msg = resolve_ref(spec, mref["$ref"])
            while isinstance(resolved_msg, dict) and "$ref" in resolved_msg and len(resolved_msg) == 1:
                resolved_msg = resolve_ref(spec, resolved_msg["$ref"])
            messages.append(resolved_msg)
        elif isinstance(mref, dict):
            messages.append(mref)

    for msg in messages:
        msg_title = msg.get("title", msg.get("name", "Message"))
        msg_summary = msg.get("summary", msg.get("description", ""))
        content_type = msg.get("contentType", spec.get("defaultContentType", "application/json"))

        lines.append(f"## Message: {msg_title}")
        lines.append("")
        lines.append(f"**Content Type:** `{content_type}`")
        lines.append("")
        if msg_summary:
            lines.append(msg_summary.strip())
            lines.append("")

        payload = msg.get("payload")
        if payload:
            lines.append("### Payload")
            lines.append("")
            table = render_property_table(payload, spec, depth=0, max_depth=2, schema_doc=schema_doc)
            if table:
                lines.append(table)
                lines.append("")

            # example
            resolved_payload = resolve_schema(spec, payload)
            example = generate_example(payload, spec)
            if example:
                lines.append("<Accordion title=\"Example payload\">")
                lines.append("")
                lines.append("````json")
                lines.append(json.dumps(example, indent=2))
                lines.append("````")
                lines.append("")
                lines.append("</Accordion>")
                lines.append("")

    return "\n".join(lines) + "\n"


def gen_messages(spec: dict, schema_doc: str | None = None) -> str:
    comp_messages = spec.get("components", {}).get("messages", {})
    if not comp_messages:
        return "# Messages\n\nNo messages defined.\n"

    lines = ["# Messages", ""]
    for name, msg in comp_messages.items():
        title = msg.get("title", msg.get("name", name))
        summary = msg.get("summary", msg.get("description", ""))
        ct = msg.get("contentType", spec.get("defaultContentType", ""))

        lines.append(f"## {title}")
        lines.append("")
        if ct:
            lines.append(f"**Content Type:** `{ct}`")
            lines.append("")
        if summary:
            lines.append(summary.strip())
            lines.append("")

        payload = msg.get("payload")
        if payload:
            lines.append("### Payload")
            lines.append("")
            table = render_property_table(payload, spec, depth=0, max_depth=1, schema_doc=schema_doc)
            if table:
                lines.append(table)
                lines.append("")
        lines.append("---")
        lines.append("")
    return "\n".join(lines) + "\n"


def gen_schemas(spec: dict, schema_doc: str | None = None) -> str:
    comp_schemas = spec.get("components", {}).get("schemas", {})
    if not comp_schemas:
        return "# Schemas\n\nNo schemas defined.\n"

    lines = ["# Schemas", ""]
    for name, schema in comp_schemas.items():
        lines.append(f"## {name}")
        lines.append("")
        resolved = resolve_schema(spec, schema)
        desc = resolved.get("description", "")
        if desc:
            lines.append(desc.strip())
            lines.append("")
        table = render_property_table(schema, spec, depth=0, max_depth=2, schema_doc=schema_doc)
        if table:
            lines.append(table)
            lines.append("")
        lines.append("---")
        lines.append("")
    return "\n".join(lines) + "\n"


# ---------------------------------------------------------------------------
# BMS operation mapping
# ---------------------------------------------------------------------------

def _build_bms_op_map(spec: dict) -> dict[str, str]:
    """Map slug suffixes to BMS operation names.

    The docs.yml BMS operation slugs follow: {objectLower}-{type}
    Operations are named: receive{Object}{Type} or publish{Object}IntegrationValue
    """
    ops = spec.get("operations", {})
    slug_map: dict[str, str] = {}
    for op_name in ops:
        # convert camelCase op name to slug: receiveAHUMetadata -> ahu-metadata
        lower = op_name
        # strip publish/receive prefix
        for prefix in ("receive", "publish"):
            if lower.startswith(prefix):
                lower = lower[len(prefix):]
                break

        # insert hyphens before uppercase runs
        slug = ""
        i = 0
        while i < len(lower):
            ch = lower[i]
            if ch.isupper():
                # find end of uppercase run
                j = i
                while j < len(lower) and lower[j].isupper():
                    j += 1
                if j - i > 1 and j < len(lower):
                    slug += ("-" if slug else "") + lower[i:j-1].lower()
                    slug += "-" + lower[j-1].lower()
                else:
                    slug += ("-" if slug else "") + lower[i:j].lower()
                i = j
            else:
                slug += lower[i]
                i += 1

        slug_map[slug] = op_name
    return slug_map


def _match_bms_operation(slug_suffix: str, op_map: dict[str, str]) -> str | None:
    """Find the BMS operation for a docs.yml slug suffix."""
    if slug_suffix in op_map:
        return op_map[slug_suffix]
    # try without hyphens
    nohyphen = slug_suffix.replace("-", "")
    for slug, op_name in op_map.items():
        if slug.replace("-", "") == nohyphen:
            return op_name
    return None


# ---------------------------------------------------------------------------
# Main generation logic
# ---------------------------------------------------------------------------

def generate_spec_pages(spec_key: str, spec: dict, dry_run: bool = False, raw_yaml: str | None = None) -> int:
    """Generate all MDX pages for a spec. Returns count of files written."""
    prefix = f"schema-{spec_key}"
    schema_doc = f"{prefix}-schemas.mdx"
    count = 0

    def _write(filename: str, content: str):
        nonlocal count
        content = escape_mdx(content)
        content = collapse_blank_lines(content)
        # Normalize to exactly one trailing newline per file.
        content = content.rstrip("\n") + "\n"
        path = DOCS / filename
        if dry_run:
            print(f"--- {filename} ({len(content)} chars) ---")
            print(content[:500])
            if len(content) > 500:
                print(f"... ({len(content) - 500} more chars)")
            print()
        else:
            path.write_text(content)
            print(f"  wrote {filename}")
        count += 1

    # Overview
    _write(f"{prefix}.mdx", gen_overview(spec, raw_yaml=raw_yaml))

    # Servers
    servers = spec.get("servers", {})
    if servers:
        _write(f"{prefix}-servers.mdx", gen_servers(spec))

    # Messages
    comp_messages = spec.get("components", {}).get("messages", {})
    if comp_messages:
        _write(f"{prefix}-messages.mdx", gen_messages(spec, schema_doc=schema_doc))

    # Schemas
    comp_schemas = spec.get("components", {}).get("schemas", {})
    if comp_schemas:
        _write(f"{prefix}-schemas.mdx", gen_schemas(spec, schema_doc=schema_doc))

    # Operations
    ops = spec.get("operations", {})

    if spec_key == "bms":
        # BMS: generate per-operation pages with slug-based naming
        op_map = _build_bms_op_map(spec)
        # find all existing MDX files that match the pattern
        existing = sorted(DOCS.glob(f"{prefix}-*.mdx"))
        for mdx_file in existing:
            stem = mdx_file.stem  # e.g. schema-bms-ahu-metadata
            # skip non-operation pages
            if stem in (f"{prefix}-messages", f"{prefix}-schemas", f"{prefix}-servers"):
                continue
            # extract slug suffix
            slug_suffix = stem[len(prefix) + 1:]  # ahu-metadata
            op_name = _match_bms_operation(slug_suffix, op_map)
            if op_name:
                title = _slug_to_title(slug_suffix)
                _write(mdx_file.name, gen_operation(spec, op_name, title, schema_doc=schema_doc))
            else:
                print(f"  WARN: no operation match for {stem} (slug: {slug_suffix})")
    else:
        # Non-BMS: one page per operation
        for op_name, op in ops.items():
            slug = op_name.lower()
            filename = f"{prefix}-{slug}.mdx"
            title = _op_name_to_title(op_name)
            _write(filename, gen_operation(spec, op_name, title, schema_doc=schema_doc))

    return count


def _slug_to_title(slug: str) -> str:
    """Convert 'ahu-metadata' to 'AHU — Metadata'."""
    parts = slug.split("-")
    # heuristic: known abbreviations stay uppercase
    abbrevs = {"ahu", "ats", "bess", "cdu", "crac", "crah", "hx", "ups", "bms"}

    result = []
    i = 0
    while i < len(parts):
        p = parts[i]
        if p in abbrevs:
            result.append(p.upper())
        elif p == "coolingtower":
            result.append("Cooling Tower")
        elif p == "genericobject":
            result.append("Generic Object")
        elif p == "powermeter":
            result.append("Power Meter")
        else:
            result.append(p.capitalize())
        i += 1

    # join with mdash between object and type
    if len(result) >= 2:
        obj = " ".join(result[:-1])
        typ = result[-1]
        return f"{obj} — {typ}"
    return " ".join(result)


def _op_name_to_title(name: str) -> str:
    """Convert 'publishLoadTargetSet' to 'Publish Load Target Set'."""
    # insert spaces before uppercase
    spaced = re.sub(r'([a-z])([A-Z])', r'\1 \2', name)
    spaced = re.sub(r'([A-Z]+)([A-Z][a-z])', r'\1 \2', spaced)
    return spaced.title()


def main():
    parser = argparse.ArgumentParser(description="Generate MDX from AsyncAPI specs")
    parser.add_argument("--spec", choices=list(SPECS.keys()), help="Generate for one spec only")
    parser.add_argument("--dry-run", action="store_true", help="Print output instead of writing files")
    args = parser.parse_args()

    specs_to_run = {args.spec: SPECS[args.spec]} if args.spec else SPECS
    total = 0

    for key, path in specs_to_run.items():
        print(f"\n=== {key} ({path.name}) ===")
        if not path.exists():
            print(f"  ERROR: {path} not found")
            continue
        raw_yaml = path.read_text()
        spec = load_yaml(path)
        if not spec:
            print(f"  ERROR: failed to parse {path}")
            continue
        total += generate_spec_pages(key, spec, dry_run=args.dry_run, raw_yaml=raw_yaml)

    print(f"\nDone. {total} files {'would be written' if args.dry_run else 'written'}.")


if __name__ == "__main__":
    main()
