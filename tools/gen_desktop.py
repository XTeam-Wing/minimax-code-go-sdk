#!/usr/bin/env python3
"""Extract DesktopService IDL from @minimax-ai/code and generate the Go client.

The extractor parses the minified IDL tables in chunk-CUW4IHOK.js. It does not
execute the package. Re-run when the pinned tarball changes:

    python3 tools/gen_desktop.py /path/to/minimax-ai-code-0.7.8.tgz

internal/inventory/categories.json supplies the planning category, phase, and
inferred side-effect label. Pass another JSON file as the second argument to
override it. Protocol fields come from the tarball. An IDL alias is not
treated as a string unless a 0.7.8 handler proves that wire type.
"""

from __future__ import annotations

import hashlib
import json
import re
import sys
import tarfile
import textwrap
from collections import OrderedDict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CHUNK = "package/chunks/chunk-CUW4IHOK.js"
BASELINE = "0.7.8"
TARBALL_SHA256 = "f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a"

ACRONYMS = {
    "id": "ID",
    "url": "URL",
    "uri": "URI",
    "api": "API",
    "http": "HTTP",
    "https": "HTTPS",
    "json": "JSON",
    "ip": "IP",
    "sse": "SSE",
    "mcp": "MCP",
    "oauth": "OAuth",
    "uid": "UID",
    "uuid": "UUID",
    "ttl": "TTL",
    "llm": "LLM",
    "im": "IM",
    "os": "OS",
    "ui": "UI",
    "sdk": "SDK",
    "pid": "PID",
}

RESERVED_TYPES = {
    "Client",
    "EventStream",
    "Scalar",
    "Number",
    "StatusError",
    "DecodeError",
    "RawResult",
    "HTTPRequest",
    "HTTPResponse",
    "HTTPStream",
    "RoundTripper",
    "MethodSpec",
    "Binding",
    "CallOption",
    "AgentShellPreference",
    "PermissionReply",
    "BatchPermissionDecision",
    "LocalHookScope",
    "InstalledPluginSource",
    "MarketplaceCategory",
    "SkillScope",
    "SkillSourceType",
    "SkillSortType",
    "CronScheduleKind",
    "CronSessionTargetMode",
    "LocalMcpTransport",
    "SandboxLocalAccess",
}

RESERVED_METHODS = {
    "Close",
    "RawCall",
    "RoundTrip",
    "RoundTripStream",
    "Inventory",
}


class ParseError(Exception):
    pass


class Parser:
    def __init__(self, src: str, i: int):
        self.s = src
        self.i = i
        self.n = len(src)

    def skip(self) -> None:
        s = self.s
        i = self.i
        n = self.n
        while i < n:
            c = s[i]
            if c in " \t\r\n":
                i += 1
                continue
            break
        self.i = i

    def parse(self):
        self.skip()
        return self.value()

    def value(self):
        self.skip()
        if self.i >= self.n:
            raise ParseError("eof")
        c = self.s[self.i]
        if c == "{":
            return self.object()
        if c == "[":
            return self.array()
        if c == '"':
            return self.string()
        if c == "-" or c.isdigit():
            return self.number()
        if self.s.startswith("true", self.i):
            self.i += 4
            return True
        if self.s.startswith("false", self.i):
            self.i += 5
            return False
        if self.s.startswith("null", self.i):
            self.i += 4
            return None
        if self.s.startswith("undefined", self.i):
            self.i += 9
            return None
        if c.isalpha() or c in "_$":
            return {"$ref": self.ident()}
        raise ParseError(f"unexpected {c!r} at {self.i}")

    def ident(self) -> str:
        i = self.i
        s = self.s
        n = self.n
        while i < n and (s[i].isalnum() or s[i] in "_$"):
            i += 1
        if i == self.i:
            raise ParseError(f"ident at {self.i}")
        out = s[self.i : i]
        self.i = i
        return out

    def string(self) -> str:
        s = self.s
        i = self.i + 1
        n = self.n
        chars = []
        while i < n:
            c = s[i]
            if c == '"':
                self.i = i + 1
                return "".join(chars)
            if c == "\\":
                i += 1
                if i >= n:
                    raise ParseError("bad escape")
                e = s[i]
                mapping = {"n": "\n", "r": "\r", "t": "\t", "b": "\b", "f": "\f", "\\": "\\", '"': '"', "/": "/"}
                if e in mapping:
                    chars.append(mapping[e])
                elif e == "u":
                    hexv = s[i + 1 : i + 5]
                    chars.append(chr(int(hexv, 16)))
                    i += 4
                else:
                    chars.append(e)
            else:
                chars.append(c)
            i += 1
        raise ParseError("unterminated string")

    def number(self):
        i = self.i
        s = self.s
        n = self.n
        if s[i] == "-":
            i += 1
        while i < n and s[i].isdigit():
            i += 1
        if i < n and s[i] == ".":
            i += 1
            while i < n and s[i].isdigit():
                i += 1
        if i < n and s[i] in "eE":
            i += 1
            if i < n and s[i] in "+-":
                i += 1
            while i < n and s[i].isdigit():
                i += 1
        raw = s[self.i : i]
        self.i = i
        if any(c in raw for c in ".eE"):
            return float(raw)
        return int(raw)

    def object(self):
        self.i += 1
        obj = OrderedDict()
        while True:
            self.skip()
            if self.i >= self.n:
                raise ParseError("unterminated object")
            if self.s[self.i] == "}":
                self.i += 1
                return obj
            if self.s[self.i] == '"':
                key = self.string()
            else:
                key = self.ident()
            self.skip()
            if self.s[self.i] != ":":
                raise ParseError(f"expected colon at {self.i}")
            self.i += 1
            obj[key] = self.value()
            self.skip()
            if self.s[self.i] == ",":
                self.i += 1
                continue
            if self.s[self.i] == "}":
                self.i += 1
                return obj
            raise ParseError(f"expected comma at {self.i}")

    def array(self):
        self.i += 1
        arr = []
        while True:
            self.skip()
            if self.s[self.i] == "]":
                self.i += 1
                return arr
            arr.append(self.value())
            self.skip()
            if self.s[self.i] == ",":
                self.i += 1
                continue
            if self.s[self.i] == "]":
                self.i += 1
                return arr
            raise ParseError(f"expected comma in array at {self.i}")


def go_export(name: str) -> str:
    parts = re.findall(r"[A-Z]+(?![a-z])|[A-Z]?[a-z]+|\d+", name)
    if not parts:
        parts = [name] if name else ["Field"]
    out = []
    for part in parts:
        low = part.lower()
        if low in ACRONYMS:
            out.append(ACRONYMS[low])
        elif part.isdigit():
            out.append(part)
        else:
            out.append(part[:1].upper() + part[1:])
    ident = "".join(out) or "Field"
    if ident[0].isdigit():
        ident = "N" + ident
    return ident


def load_chunk(tarball: Path) -> str:
    digest = hashlib.sha256(tarball.read_bytes()).hexdigest()
    if digest != TARBALL_SHA256:
        raise SystemExit(f"tarball sha256 {digest} != pinned {TARBALL_SHA256}")
    with tarfile.open(tarball) as tf:
        member = tf.getmember(CHUNK)
        raw = tf.extractfile(member).read()
    return raw.decode("utf-8")


def extract(src: str):
    schemas_by_ident = {}
    schemas_by_name = {}
    for match in re.finditer(r"([A-Za-z_$][\w$]*)=\{name:\"([^\"]+)\",fields:", src):
        ident = match.group(1)
        brace = match.end() - len('fields:') - 1
        # match ends at fields: so brace is the '{' of {name:
        brace = src.rfind("{", match.start(), match.end())
        parser = Parser(src, brace)
        try:
            obj = parser.parse()
        except ParseError as exc:
            raise SystemExit(f"schema {ident} parse failed: {exc}") from exc
        if not isinstance(obj, dict) or obj.get("name") != match.group(2):
            continue
        schemas_by_ident[ident] = obj
        schemas_by_name.setdefault(obj["name"], obj)

    methods = []
    for match in re.finditer(r"\{serviceName:\"DesktopService\"", src):
        parser = Parser(src, match.start())
        try:
            obj = parser.parse()
        except ParseError as exc:
            raise SystemExit(f"method parse failed at {match.start()}: {exc}") from exc
        if obj.get("serviceName") != "DesktopService" or "methodName" not in obj:
            continue
        methods.append(obj)

    ref_names = set(re.findall(r"structFields:([A-Za-z_$][\w$]*)", src))
    arrays = {}
    for name in sorted(ref_names):
        token = f"{name}=[{{thriftName:"
        at = src.find(token)
        if at < 0:
            token = f"{name}=["
            at = src.find(token)
            if at < 0:
                continue
        parser = Parser(src, at + len(name) + 1)
        try:
            val = parser.parse()
        except ParseError:
            continue
        if isinstance(val, list):
            arrays[name] = val
    return schemas_by_ident, schemas_by_name, methods, arrays


def resolve_ref(node, arrays, schemas_by_ident):
    if isinstance(node, dict) and "$ref" in node and len(node) == 1:
        name = node["$ref"]
        if name in arrays:
            return arrays[name]
        if name in schemas_by_ident:
            return schemas_by_ident[name]
        return node
    return node


def binding_children(binding, arrays):
    fields = binding.get("structFields")
    fields = resolve_ref(fields, arrays, {})
    if not isinstance(fields, list):
        return []
    out = []
    for field in fields:
        if not isinstance(field, dict) or "fieldName" not in field:
            continue
        out.append(
            {
                "field": field.get("fieldName"),
                "wire": field.get("wireName") or field.get("jsonName") or field.get("fieldName"),
                "json": field.get("jsonName") or field.get("wireName") or field.get("fieldName"),
                "requiredness": field.get("requiredness") or "optional",
                "tsType": field.get("tsType") or "",
                "valueKind": field.get("valueKind") or "scalar",
            }
        )
    return out


def schema_field_map(schema):
    if not schema:
        return {}
    out = {}
    for field in schema.get("fields") or []:
        if isinstance(field, dict) and field.get("fieldName"):
            out[field["fieldName"]] = field
    return out


def value_type_name(value, schemas_by_ident):
    if not isinstance(value, dict):
        return None
    kind = value.get("kind")
    if kind == "struct":
        schema = resolve_ref(value.get("schema"), {}, schemas_by_ident)
        if isinstance(schema, dict) and schema.get("name"):
            return schema["name"]
        if isinstance(value.get("schema"), dict) and "$ref" in value["schema"]:
            return None
    return None


def simplify_methods(methods, schemas_by_name, schemas_by_ident, arrays):
    simplified = []
    names = []
    for method in methods:
        bindings_in = method.get("bindings") or []
        request_schema_name = method.get("requestType")
        request_schema = schemas_by_name.get(request_schema_name) or {}
        fields = schema_field_map(request_schema)
        bindings = []
        type_only = []
        for binding in bindings_in:
            kind = binding.get("kind")
            if kind == "none":
                type_only.append(
                    {
                        "field": binding.get("fieldName"),
                        "tsType": binding.get("tsType") or "",
                        "valueKind": binding.get("valueKind") or "",
                        "nestedStruct": binding.get("tsType") if binding.get("valueKind") == "struct" else None,
                        "children": binding_children(binding, arrays),
                    }
                )
                continue
            field_name = binding.get("fieldName")
            schema_field = fields.get(field_name) or {}
            schema_value = schema_field.get("value") if isinstance(schema_field, dict) else None
            nested = value_type_name(schema_value, schemas_by_ident)
            item_name = None
            map_name = None
            container = None
            if isinstance(schema_value, dict):
                container = schema_value.get("kind")
                if container in ("list", "set"):
                    item_name = value_type_name(schema_value.get("item"), schemas_by_ident)
                elif container == "map":
                    map_name = value_type_name(schema_value.get("value"), schemas_by_ident)
            bindings.append(
                {
                    "field": field_name,
                    "wire": binding.get("wireName") or "",
                    "json": binding.get("jsonName") or binding.get("wireName") or field_name,
                    "kind": kind,
                    "requiredness": binding.get("requiredness") or "optional",
                    "tsType": binding.get("tsType") or "",
                    "valueKind": binding.get("valueKind") or "scalar",
                    "schemaContainer": container,
                    "nestedStruct": nested,
                    "itemStruct": item_name,
                    "mapStruct": map_name,
                    "children": binding_children(binding, arrays),
                }
            )
        kinds = [b.get("kind") for b in bindings_in]
        whole = method.get("method") != "GET" and all(k == "implicitBody" for k in kinds)
        path = method["path"]
        simplified.append(
            {
                "inventoryID": f"DesktopService.{method['methodName']}",
                "methodName": method["methodName"],
                "clientKey": None,
                "httpMethod": method["method"],
                "path": path,
                "requestType": method.get("requestType"),
                "responseType": method.get("responseType"),
                "frameType": method.get("frameType"),
                "errorBodyType": method.get("errorBodyType"),
                "transport": method.get("transport"),
                "wholeBody": bool(whole),
                "gatewayOverride": path == "/minimax-desktop/api/v1/drive"
                or path.startswith("/minimax-desktop/api/v1/drive/"),
                "bindings": bindings,
                "typeOnly": type_only,
            }
        )
        names.append(method["methodName"])
    if len(names) != len(set(names)):
        raise SystemExit("duplicate method names")
    if len(simplified) != 470:
        raise SystemExit(f"expected 470 methods, got {len(simplified)}")
    return simplified


def reachable_type_names(methods, schemas_by_name, schemas_by_ident):
    needed = []
    seen = set()

    def add(name):
        if not name or name in seen:
            return
        if name not in schemas_by_name:
            return
        seen.add(name)
        needed.append(name)
        schema = schemas_by_name[name]
        for field in schema.get("fields") or []:
            walk(field.get("value"))

    def walk(value):
        if not isinstance(value, dict):
            return
        kind = value.get("kind")
        if kind == "struct":
            schema = resolve_ref(value.get("schema"), {}, schemas_by_ident)
            if isinstance(schema, dict):
                add(schema.get("name"))
        elif kind in ("list", "set"):
            walk(value.get("item"))
        elif kind == "map":
            walk(value.get("value"))

    for method in methods:
        add(method["responseType"])
        add(method["frameType"])
        add(method["errorBodyType"])
        add(method["requestType"])
        for binding in method["bindings"]:
            if binding.get("nestedStruct"):
                add(binding["nestedStruct"])
            if binding.get("itemStruct"):
                add(binding["itemStruct"])
            if binding.get("mapStruct"):
                add(binding["mapStruct"])
    return needed


def go_type_name(schema_name: str, used: dict[str, str]) -> str:
    name = go_export(schema_name)
    if name in RESERVED_TYPES:
        name = "IDL" + name
    base = name
    n = 2
    while name in used.values() and used.get(schema_name) != name:
        # collision with a different schema
        if any(v == name and k != schema_name for k, v in used.items()):
            name = f"{base}{n}"
            n += 1
        else:
            break
    used[schema_name] = name
    return name


EVIDENCE: dict[tuple[str, str], set[str]] = {}


def add_evidence(schema: str | None, field: str | None, ts: str | None) -> None:
    if schema and field and ts:
        EVIDENCE.setdefault((schema, field), set()).add(ts)


def collect_evidence(methods) -> None:
    EVIDENCE.clear()
    for method in methods:
        groups = list(method["bindings"]) + list(method.get("typeOnly") or [])
        for binding in groups:
            add_evidence(method.get("requestType"), binding.get("field"), binding.get("tsType"))
            parents = []
            if binding.get("nestedStruct"):
                parents.append(binding["nestedStruct"])
            if binding.get("valueKind") == "struct" and binding.get("tsType"):
                parents.append(binding["tsType"])
            for parent in parents:
                for child in binding.get("children") or []:
                    add_evidence(parent, child.get("field"), child.get("tsType"))


# Go types in desktop/aliases.go. Each entry was checked against a 0.7.8
# handler, not inferred from the alias spelling.
PROVEN_STRING_ALIAS = {}
PROVEN_INT_ALIAS = {
    "AgentShellPreference": "AgentShellPreference",
    "PermissionReply": "PermissionReply",
    "BatchPermissionDecision": "BatchPermissionDecision",
    "LocalHookScope": "LocalHookScope",
    "InstalledPluginSource": "InstalledPluginSource",
    "MarketplaceCategory": "MarketplaceCategory",
    "SkillScope": "SkillScope",
    "SkillSourceType": "SkillSourceType",
    "SkillSortType": "SkillSortType",
    "CronScheduleKind": "CronScheduleKind",
    "CronSessionTargetMode": "CronSessionTargetMode",
    "LocalMcpTransport": "LocalMcpTransport",
    "SandboxLocalAccess": "SandboxLocalAccess",
}
# JSON 0 is an accepted wire value, so a required field is still a pointer.
# Otherwise the Go zero value would send that choice (allowOnce, Recurring, New).
ZERO_IS_VALID_ALIAS = {
    "AgentShellPreference",
    "PermissionReply",
    "BatchPermissionDecision",
    "CronScheduleKind",
    "CronSessionTargetMode",
    "MarketplaceCategory",
}
# SessionKind accepts either a known string or the numeric enum. The other two
# names have no recovered reader. All three stay Scalar.
LOSSLESS_ALIAS = {
    "SessionKind",
    "CloudCronScheduleKind",
    "WorkShareKind",
}


def go_from_ts(ts: str, optional: bool) -> str | None:
    if ts == "string":
        return "*string" if optional else "string"
    if ts in ("number", "ThriftI64"):
        return "*Number" if optional else "Number"
    if ts == "boolean":
        return "*bool" if optional else "bool"
    if ts == "string[]":
        return "[]string"
    if ts in ("number[]", "ThriftI64[]"):
        return "[]Number"
    if ts == "boolean[]":
        return "[]bool"
    if ts == "string[][]":
        return "[][]string"
    if ts == "Record<string, string>":
        return "map[string]string"
    if ts == "Record<string, Record<string, string>>":
        return "map[string]map[string]string"
    if ts in PROVEN_STRING_ALIAS:
        go = PROVEN_STRING_ALIAS[ts]
        return f"*{go}" if optional else go
    if ts in PROVEN_INT_ALIAS:
        go = PROVEN_INT_ALIAS[ts]
        if optional or ts in ZERO_IS_VALID_ALIAS:
            return f"*{go}"
        return go
    if ts in LOSSLESS_ALIAS:
        return "*Scalar" if optional else "Scalar"
    # An unrecognized alias is not a string. Keep the JSON literal.
    if ts and ts[0].isupper() and not ts.endswith("[]") and "<" not in ts:
        return "*Scalar" if optional else "Scalar"
    return None


def evidence_type(schema_name: str, field_name: str, optional: bool) -> str | None:
    types = EVIDENCE.get((schema_name, field_name))
    if not types or len(types) != 1:
        return None
    return go_from_ts(next(iter(types)), optional)


def field_go_type(field, type_names, optional: bool, schema_name: str = "") -> str:
    value = field.get("value") or {}
    kind = value.get("kind")
    if kind == "scalar" or kind is None:
        base = evidence_type(schema_name, field.get("fieldName") or "", optional) or "Scalar"
    elif kind == "struct":
        ref = value.get("schema")
        nested = None
        if isinstance(ref, dict) and ref.get("name"):
            nested = type_names.get(ref["name"])
        elif isinstance(ref, dict) and "$ref" in ref:
            nested = None
        base = f"*{nested}" if nested else "json.RawMessage"
        if optional:
            return base if base.startswith("*") or base == "json.RawMessage" else f"*{base}"
        return base
    elif kind in ("list", "set"):
        item = value.get("item") or {}
        if item.get("kind") == "struct":
            ref = item.get("schema")
            nested = ref.get("name") if isinstance(ref, dict) else None
            go = type_names.get(nested, "")
            base = f"[]{go}" if go else "[]json.RawMessage"
        elif item.get("kind") == "scalar" or item.get("kind") is None:
            hinted = evidence_type(schema_name, field.get("fieldName") or "", False)
            base = hinted if hinted and hinted.startswith("[]") else "[]Scalar"
        else:
            base = "[]json.RawMessage"
    elif kind == "map":
        hinted = evidence_type(schema_name, field.get("fieldName") or "", False)
        if hinted and hinted.startswith("map["):
            base = hinted
            if optional and kind in ("scalar", None) and base in ("Scalar",):
                return "*Scalar"
            return base
        inner = value.get("value") or {}
        if inner.get("kind") == "struct":
            ref = inner.get("schema")
            nested = ref.get("name") if isinstance(ref, dict) else None
            go = type_names.get(nested, "")
            base = f"map[string]*{go}" if go else "map[string]json.RawMessage"
        else:
            base = "map[string]Scalar"
    else:
        base = "json.RawMessage"
    if optional and kind in ("scalar", None) and base in ("Scalar",):
        return "*Scalar"
    return base


def binding_go_type(binding, type_names) -> str:
    optional = binding["requiredness"] != "required"
    container = binding.get("schemaContainer")
    vk = binding.get("valueKind")
    ts = binding.get("tsType") or ""

    def scalar_from_ts(ts_type: str) -> str:
        mapped = go_from_ts(ts_type, optional)
        if mapped:
            return mapped
        return "*Scalar" if optional else "Scalar"

    # Schema shape wins over valueKind "unsupported". The HTTP encoder
    # serializes body fields from the schema, and "unsupported" only means
    # the binding's scalar coercion does not apply.
    if ts == "string[][]":
        return "[][]string"
    if binding.get("itemStruct"):
        return f"[]{type_names[binding['itemStruct']]}"
    if binding.get("mapStruct"):
        return f"map[string]*{type_names[binding['mapStruct']]}"
    if binding.get("nestedStruct") and type_names.get(binding["nestedStruct"]):
        return f"*{type_names[binding['nestedStruct']]}"
    if container in ("list", "set") or vk == "scalarList" or ts.endswith("[]"):
        inner = ts[:-2] if ts.endswith("[]") else ""
        if inner.startswith("(") and inner.endswith(")"):
            inner = inner[1:-1]
        if inner == "string":
            return "[]string"
        if inner in ("number", "ThriftI64"):
            return "[]Number"
        if inner == "boolean":
            return "[]bool"
        if inner in type_names:
            return f"[]{type_names[inner]}"
        if vk == "unsupported":
            return "json.RawMessage"
        return "[]Scalar"
    if container == "map" or ts.startswith("Record<"):
        if ts == "Record<string, string>":
            return "map[string]string"
        if ts == "Record<string, Record<string, string>>":
            return "map[string]map[string]string"
        if vk == "unsupported":
            return "json.RawMessage"
        return "map[string]Scalar"
    if container == "struct" or vk == "struct":
        go = type_names.get(ts)
        if go:
            return f"*{go}"
        return "json.RawMessage"
    if vk == "unsupported":
        return "json.RawMessage"
    if vk == "scalar" or container == "scalar" or container is None:
        return scalar_from_ts(ts)
    return "json.RawMessage"


def assign_type_names(needed, schemas_by_name):
    used = {}
    # stable
    for name in needed:
        go_type_name(name, used)
    # second pass to resolve collisions deterministically
    seen = {}
    final = {}
    for name in needed:
        base = go_export(name)
        if base in RESERVED_TYPES:
            base = "IDL" + base
        candidate = base
        n = 2
        while candidate in seen.values():
            candidate = f"{base}{n}"
            n += 1
        seen[name] = candidate
        final[name] = candidate
    return final


def render_struct(schema_name, go_name, schema, type_names, gateway: bool) -> str:
    lines = [f"// {go_name} is the 0.7.8 IDL struct {schema_name}."]
    lines.append("// Scalar fields preserve JSON literals whose primitive type is not declared in the IDL.")
    if gateway:
        lines.append("// GatewayRaw is the response body before IDL decoding. Drive routes are rewritten to the website gateway, which does not run the IDL response encoder.")
    lines.append(f"type {go_name} struct {{")
    used_fields = set()
    for field in schema.get("fields") or []:
        fname = field.get("fieldName")
        if not fname:
            continue
        go_field = go_export(fname)
        base = go_field
        n = 2
        while go_field in used_fields:
            go_field = f"{base}{n}"
            n += 1
        used_fields.add(go_field)
        json_name = field.get("jsonName") or field.get("wireName") or fname
        optional = field.get("requiredness") != "required"
        go_t = field_go_type(field, type_names, optional, schema_name)
        # resolve struct names inside field_go_type via schema objects already named
        tag = json_name
        if optional:
            tag += ",omitempty"
        lines.append(f"\t{go_field} {go_t} `json:\"{tag}\"`")
    if gateway:
        lines.append("\tGatewayRaw json.RawMessage `json:\"-\"`")
    lines.append("}")
    if gateway:
        lines.append("")
        lines.append(f"func (r *{go_name}) setGatewayRaw(b []byte) {{ r.GatewayRaw = append([]byte(nil), b...) }}")
    return "\n".join(lines)


def fix_field_types(schema, type_names):
    """field_go_type looks up nested schema by object identity name; ensure schema refs are dicts with name."""
    return schema


def prepare_schemas(schemas_by_ident):
    """Inline schema $ref so field_go_type can read .name."""
    seen: set[int] = set()
    for schema in schemas_by_ident.values():
        inline_schema(schema, schemas_by_ident, seen)


def inline_schema(schema, schemas_by_ident, seen: set[int]) -> None:
    if not isinstance(schema, dict):
        return
    marker = id(schema)
    if marker in seen:
        return
    seen.add(marker)
    for field in schema.get("fields") or []:
        inline_value(field.get("value"), schemas_by_ident, seen)


def inline_value(value, schemas_by_ident, seen: set[int]) -> None:
    if not isinstance(value, dict):
        return
    kind = value.get("kind")
    if kind == "struct":
        ref = value.get("schema")
        if isinstance(ref, dict) and "$ref" in ref and "name" not in ref:
            target = schemas_by_ident.get(ref["$ref"])
            if target:
                value["schema"] = target
                ref = target
        if isinstance(ref, dict) and ref.get("fields"):
            inline_schema(ref, schemas_by_ident, seen)
    elif kind in ("list", "set"):
        inline_value(value.get("item"), schemas_by_ident, seen)
    elif kind == "map":
        inline_value(value.get("value"), schemas_by_ident, seen)


def render_request_struct(method, go_req: str, type_names) -> str:
    lines = [
        f"// {go_req} is the request for {method['methodName']}.",
        "// Field types come from IDL bindings (tsType) and nested schema names.",
        "// Optional values use pointers so an unset field is omitted. Kind \"none\" bindings, including base_req, are not sent and are omitted here.",
        f"type {go_req} struct {{",
    ]
    used = set()
    for binding in method["bindings"]:
        go_field = go_export(binding["field"])
        base = go_field
        n = 2
        while go_field in used:
            go_field = f"{base}{n}"
            n += 1
        used.add(go_field)
        binding["goField"] = go_field
        go_t = binding_go_type(binding, type_names)
        json_name = binding["json"] or binding["wire"] or binding["field"]
        optional = binding["requiredness"] != "required"
        tag = json_name
        if optional:
            tag += ",omitempty"
        lines.append(f"\t{go_field} {go_t} `json:\"{tag}\"`")
    lines.append("}")
    return "\n".join(lines)


def go_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def render_specs(methods) -> str:
    chunks = ["func init() {"]
    for method in methods:
        bindings = []
        for binding in method["bindings"]:
            children = []
            for child in binding["children"]:
                children.append(
                    "childBinding{Field: %s, Wire: %s, JSON: %s, Required: %s, TSType: %s, ValueKind: %s}"
                    % (
                        go_string(go_export(child["field"])),
                        go_string(child["wire"]),
                        go_string(child["json"]),
                        "true" if child["requiredness"] == "required" else "false",
                        go_string(child["tsType"]),
                        go_string(child["valueKind"]),
                    )
                )
            child_src = "nil"
            if children:
                child_src = "[]childBinding{" + ", ".join(children) + "}"
            bindings.append(
                "bindingSpec{Field: %s, Wire: %s, JSON: %s, Kind: %s, Required: %s, TSType: %s, ValueKind: %s, Children: %s}"
                % (
                    go_string(binding["goField"]),
                    go_string(binding["wire"]),
                    go_string(binding["json"]),
                    go_string(binding["kind"]),
                    "true" if binding["requiredness"] == "required" else "false",
                    go_string(binding["tsType"]),
                    go_string(binding["valueKind"]),
                    child_src,
                )
            )
        bind_src = "nil"
        if bindings:
            bind_src = "[]bindingSpec{\n\t\t\t" + ",\n\t\t\t".join(bindings) + ",\n\t\t}"
        chunks.append(
            "\tspecByName[%s] = methodSpec{Name: %s, HTTPMethod: %s, Path: %s, RequestType: %s, ResponseType: %s, FrameType: %s, ErrorBodyType: %s, Transport: %s, WholeBody: %s, GatewayOverride: %s, Bindings: %s, SideEffect: %s, Category: %s, Phase: %s}"
            % (
                go_string(method["methodName"]),
                go_string(method["methodName"]),
                go_string(method["httpMethod"]),
                go_string(method["path"]),
                go_string(method["requestType"] or ""),
                go_string(method["responseType"] or ""),
                go_string(method["frameType"] or ""),
                go_string(method["errorBodyType"] or ""),
                go_string(method["transport"] or ""),
                "true" if method["wholeBody"] else "false",
                "true" if method["gatewayOverride"] else "false",
                bind_src,
                go_string(method.get("sideEffect") or ""),
                go_string(method.get("category") or ""),
                go_string(method.get("phase") or ""),
            )
        )
    chunks.append("}")
    return "\n".join(chunks)


def render_method(method, req_name, resp_name, frame_name) -> str:
    docs = [
        f"// {method['methodName']} calls {method['httpMethod']} {method['path']}.",
        f"// Inventory {method['inventoryID']}. Transport {method['transport']}.",
    ]
    if method["transport"] == "sse":
        docs.append(f"// Frames decode as {frame_name}. data: [DONE] ends the stream. HTTP errors are not frames.")
    else:
        docs.append("// Response field names follow the IDL jsonName. Scalar primitives without a TypeScript type stay raw JSON.")
    if method["gatewayOverride"]:
        docs.append("// This path is rewritten by the app-server dispatcher to the website gateway before the IDL registrar. GatewayRaw keeps the original body.")
    if method.get("sideEffect") in ("mutating", "destructive", "approval", "auth_credential"):
        docs.append("// The SDK does not replay this call after a lost response or reconnect.")
    if method.get("sideEffect") == "approval":
        docs.append("// A nil permission handler must not be treated as approval. This method sends a decision only when the caller sets one.")
    body = "\n".join(docs) + "\n"
    if method["transport"] == "sse":
        body += (
            f"func (c *Client) {method['methodName']}(ctx context.Context, req {req_name}) (*EventStream[{frame_name}], error) {{\n"
            f"\treturn callStream[{frame_name}](c, ctx, specByName[{go_string(method['methodName'])}], req)\n"
            f"}}\n"
        )
    else:
        result = "DriveGatewayResp" if method.get("gatewayOverride") else resp_name
        if method.get("gatewayOverride"):
            body += (
                f"func (c *Client) {method['methodName']}(ctx context.Context, req {req_name}) (DriveGatewayResp, error) {{\n"
                f"\tif err := validateDriveGateway({go_string(method['methodName'])}, req); err != nil {{\n"
                f"\t\treturn DriveGatewayResp{{}}, err\n"
                f"\t}}\n"
                f"\treturn callUnary[DriveGatewayResp](c, ctx, specByName[{go_string(method['methodName'])}], req)\n"
                f"}}\n"
            )
        else:
            body += (
                f"func (c *Client) {method['methodName']}(ctx context.Context, req {req_name}) ({result}, error) {{\n"
                f"\treturn callUnary[{result}](c, ctx, specByName[{go_string(method['methodName'])}], req)\n"
                f"}}\n"
            )
    return body


def render_catalog(methods) -> str:
    lines = ["// MethodCatalog is every DesktopService method generated from the pinned 0.7.8 IDL.", "func MethodCatalog() []MethodInfo {", "\treturn []MethodInfo{"]
    for method in methods:
        lines.append(
            "\t\t{InventoryID: %s, Name: %s, HTTPMethod: %s, Path: %s, Transport: %s, RequestType: %s, ResponseType: %s, FrameType: %s, ErrorBodyType: %s, WholeBody: %s, GatewayOverride: %s, Category: %s, Phase: %s, SideEffect: %s},"
            % (
                go_string(method["inventoryID"]),
                go_string(method["methodName"]),
                go_string(method["httpMethod"]),
                go_string(method["path"]),
                go_string(method["transport"] or ""),
                go_string(method["requestType"] or ""),
                go_string(method["responseType"] or ""),
                go_string(method["frameType"] or ""),
                go_string(method["errorBodyType"] or ""),
                "true" if method["wholeBody"] else "false",
                "true" if method["gatewayOverride"] else "false",
                go_string(method.get("category") or ""),
                go_string(method.get("phase") or ""),
                go_string(method.get("sideEffect") or ""),
            )
        )
    lines.append("\t}")
    lines.append("}")
    return "\n".join(lines)


def render_inventory_md(methods) -> str:
    lines = [
        "# DesktopService method inventory",
        "",
        "Generated from `@minimax-ai/code` 0.7.8 IDL tables (`k` unary and `Yi` SSE) cross-checked against the registrar call shape. This is not an OpenAI Codex wire protocol.",
        "",
        f"Denominator: **{len(methods)}** IDL methods. `RawCall` is not one of them. JSON-RPC outer methods, server events, and mavis dispatcher leaves are outside this table. A live coverage count is a generated report, not a number stored here.",
        "",
        "Scalar means the JSON literal is preserved. That is either a schema `kind: scalar` with no single binding or structFields tsType, or an alias whose wire primitive was not recovered. Aliases are typed only when a 0.7.8 handler shows the JSON value. `SessionKind` stays Scalar because the server accepts both the strings conversation, task, peek, channel, cron, unknown and the numbers 0 through 5. `CloudCronScheduleKind` and `WorkShareKind` stay Scalar because no reader was recovered.",
        "",
        "Drive paths are rewritten to the website gateway before the IDL response encoder. Those methods return `DriveGatewayResp` (`base_resp` plus the raw JSON body). The IDL response struct is not applied on that path.",
        "",
        "| Method | HTTP | Path | Transport | Category | Phase | Side effect |",
        "| --- | --- | --- | --- | --- | --- | --- |",
    ]
    for method in methods:
        lines.append(
            f"| `{method['methodName']}` | {method['httpMethod']} | `{method['path']}` | {method['transport']} | {method.get('category') or ''} | {method.get('phase') or ''} | {method.get('sideEffect') or ''} |"
        )
    lines.append("")
    return "\n".join(lines)


def merge_research(methods, research_path: Path | None):
    if research_path is None or not research_path.exists():
        for method in methods:
            method["category"] = category_from_path(method["path"])
            method["phase"] = ""
            method["sideEffect"] = "read" if method["httpMethod"] == "GET" and method["transport"] != "sse" else "mutating"
        return
    research = json.loads(research_path.read_text())
    by_name = {item["method_name"]: item for item in research}
    missing = []
    extra = set(by_name) - {m["methodName"] for m in methods}
    for method in methods:
        item = by_name.get(method["methodName"])
        if not item:
            missing.append(method["methodName"])
            method["category"] = category_from_path(method["path"])
            method["phase"] = ""
            method["sideEffect"] = "read" if method["httpMethod"] == "GET" else "mutating"
            continue
        if item["http_method"] != method["httpMethod"] or item["path"] != method["path"]:
            raise SystemExit(f"research mismatch for {method['methodName']}: {item['http_method']} {item['path']} vs {method['httpMethod']} {method['path']}")
        method["category"] = item.get("category") or ""
        method["phase"] = item.get("phase") or ""
        method["sideEffect"] = item.get("side_effect_inferred") or ""
    if missing:
        raise SystemExit(f"research missing {len(missing)} methods, first {missing[:5]}")
    if extra:
        raise SystemExit(f"research has {len(extra)} methods not in IDL, first {list(extra)[:5]}")


def category_from_path(path: str) -> str:
    return "unclassified"


def emit(methods, schemas_by_name, type_names, needed):
    gateway_resps = {m["responseType"] for m in methods if m["gatewayOverride"] and m.get("responseType")}
    type_chunks = [
        "// Code generated by tools/gen_desktop.py from @minimax-ai/code 0.7.8. DO NOT EDIT.",
        "",
        "package desktop",
        "",
        "import \"encoding/json\"",
        "",
    ]
    for name in needed:
        schema = schemas_by_name[name]
        go_name = type_names[name]
        # Request structs are rendered from bindings, not the raw schema, when a method uses this name as requestType.
        if any(m["requestType"] == name for m in methods):
            continue
        gateway = name in gateway_resps
        type_chunks.append(render_struct(name, go_name, schema, type_names, gateway))
        type_chunks.append("")
    # Request structs. Several methods share one request type and the same bindings.
    rendered_requests: dict[str, list] = {}
    for method in methods:
        req_schema = method["requestType"] or f"{method['methodName']}Req"
        go_req = type_names.get(req_schema) or go_export(req_schema)
        if req_schema not in type_names:
            type_names[req_schema] = go_req
        previous = rendered_requests.get(go_req)
        if previous is not None:
            if [b["field"] for b in previous] != [b["field"] for b in method["bindings"]]:
                raise SystemExit(f"shared request {req_schema} has different bindings")
            for left, right in zip(method["bindings"], previous):
                left["goField"] = right["goField"]
            continue
        type_chunks.append(render_request_struct(method, go_req, type_names))
        type_chunks.append("")
        rendered_requests[go_req] = method["bindings"]
        if method["transport"] != "sse":
            resp = method["responseType"]
            if resp and resp not in schemas_by_name:
                go_resp = type_names.get(resp) or go_export(resp)
                type_names[resp] = go_resp
                type_chunks.append(f"type {go_resp} struct {{}}\n")

    method_chunks = [
        "// Code generated by tools/gen_desktop.py from @minimax-ai/code 0.7.8. DO NOT EDIT.",
        "",
        "package desktop",
        "",
        "import \"context\"",
        "",
    ]
    for method in methods:
        req = type_names[method["requestType"]]
        if method["transport"] == "sse":
            frame = type_names.get(method["frameType"]) or "json.RawMessage"
            # frame type lives in type_names; if missing use Scalar placeholder struct
            if method["frameType"] not in type_names:
                raise SystemExit(f"missing frame type {method['frameType']}")
            frame = type_names[method["frameType"]]
            resp = frame
        else:
            if method["responseType"] not in type_names:
                raise SystemExit(f"missing response {method['responseType']}")
            resp = type_names[method["responseType"]]
            frame = resp
        go_method = method["methodName"]
        if go_method in RESERVED_METHODS:
            raise SystemExit(f"method name collides with client API: {go_method}")
        method_chunks.append(render_method(method, req, resp, frame))

    catalog = [
        "// Code generated by tools/gen_desktop.py from @minimax-ai/code 0.7.8. DO NOT EDIT.",
        "",
        "package desktop",
        "",
        "var specByName = map[string]methodSpec{}",
        "",
        render_specs(methods),
        "",
        render_catalog(methods),
        "",
    ]
    (ROOT / "desktop" / "zz_types_gen.go").write_text("\n".join(type_chunks) + "\n")
    (ROOT / "desktop" / "zz_methods_gen.go").write_text("\n".join(method_chunks) + "\n")
    (ROOT / "desktop" / "zz_catalog_gen.go").write_text("\n".join(catalog) + "\n")


def public_inventory(methods):
    out = []
    for method in methods:
        out.append(
            {
                "inventory_id": method["inventoryID"],
                "method_name": method["methodName"],
                "http_method": method["httpMethod"],
                "path": method["path"],
                "transport": method["transport"],
                "request_type": method["requestType"],
                "response_type": method["responseType"],
                "frame_type": method["frameType"],
                "error_body_type": method["errorBodyType"],
                "whole_body": method["wholeBody"],
                "gateway_override": method["gatewayOverride"],
                "category": method.get("category"),
                "phase": method.get("phase"),
                "side_effect_inferred": method.get("sideEffect"),
                "bindings": method["bindings"],
            }
        )
    return out


def main(argv: list[str]) -> None:
    if len(argv) < 2:
        raise SystemExit("usage: gen_desktop.py <tarball> [research.json]")
    tarball = Path(argv[1])
    research = Path(argv[2]) if len(argv) > 2 else ROOT / "internal" / "inventory" / "categories.json"
    src = load_chunk(tarball)
    schemas_by_ident, schemas_by_name, raw_methods, arrays = extract(src)
    prepare_schemas(schemas_by_ident)
    # names map may point at inlined schemas; refresh by name from ident values
    schemas_by_name = {}
    for schema in schemas_by_ident.values():
        schemas_by_name.setdefault(schema["name"], schema)
    methods = simplify_methods(raw_methods, schemas_by_name, schemas_by_ident, arrays)
    merge_research(methods, research)
    needed = reachable_type_names(methods, schemas_by_name, schemas_by_ident)
    # request types that are also schemas stay in type_names but are rendered from bindings
    collect_evidence(methods)
    type_names = assign_type_names(needed, schemas_by_name)
    for method in methods:
        if method["requestType"] and method["requestType"] not in type_names:
            type_names[method["requestType"]] = go_export(method["requestType"])
            if type_names[method["requestType"]] in RESERVED_TYPES:
                type_names[method["requestType"]] = "IDL" + type_names[method["requestType"]]
    emit(methods, schemas_by_name, type_names, needed)
    inventory = {
        "artifact": {
            "package": "@minimax-ai/code",
            "version": BASELINE,
            "tarball_sha256": TARBALL_SHA256,
            "chunk": CHUNK,
        },
        "denominators": {
            "desktop_service_methods": len(methods),
            "sse": sum(1 for m in methods if m["transport"] == "sse"),
            "unary": sum(1 for m in methods if m["transport"] != "sse"),
            "gateway_override": sum(1 for m in methods if m["gatewayOverride"]),
            "whole_body": sum(1 for m in methods if m["wholeBody"]),
        },
        "methods": public_inventory(methods),
    }
    inv_path = ROOT / "internal" / "inventory" / "desktop_service.json"
    inv_path.write_text(json.dumps(inventory, indent=2, ensure_ascii=False) + "\n")
    (ROOT / "docs" / "method-inventory.md").write_text(render_inventory_md(methods))
    print(json.dumps(inventory["denominators"], indent=2))
    print("types", len(needed), "wrote", inv_path)


if __name__ == "__main__":
    main(sys.argv)
