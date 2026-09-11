"""Validate the versioned fixtures against a fresh Codex app-server schema.

Usage: python3 validate_protocol.py /path/to/generated-schema
Requires the standard jsonschema package. Does not call a model or use credentials.
"""
import json
import sys
from pathlib import Path

import jsonschema

fixtures = json.loads(Path(__file__).with_name("protocol-0.154.0.json").read_text())
schema = json.loads((Path(sys.argv[1]) / "ServerNotification.json").read_text())
validator = jsonschema.validators.validator_for(schema)
inventory = {
    entry["properties"]["type"]["enum"][0]
    for entry in schema["definitions"]["ThreadItem"]["oneOf"]
}
covered = {entry["wire"]["type"] for entry in fixtures["items"]}
assert inventory == covered, f"Item coverage drift: missing={inventory - covered}, removed={covered - inventory}"
error_variants = set()
for entry in schema["definitions"]["CodexErrorInfo"]["oneOf"]:
    error_variants.update(entry.get("enum", entry.get("required", [])))
covered_errors = {entry["name"] for entry in fixtures["errors"]}
assert error_variants <= covered_errors, f"Missing errors: {error_variants - covered_errors}"
for category, definition in (("items", "ThreadItem"), ("errors", "TurnError"), ("notifications", None)):
    target = schema if definition is None else {
        "$ref": "#/definitions/" + definition,
        "definitions": schema["definitions"],
    }
    check = validator(target)
    for entry in fixtures[category]:
        check.validate(entry["wire"])
    print(f"Validated {len(fixtures[category])} {category}")
