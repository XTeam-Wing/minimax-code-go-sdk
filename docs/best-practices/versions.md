# Versions and tests

This module targets `@minimax-ai/code` 0.7.8 and Go 1.22 or newer. `StrictBaseline` fails the handshake when `serverInfo.version` is not `0.7.8`.

The pinned tarball SHA-256 is `f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a`. Regenerate the DesktopService bindings with:

```bash
python3 tools/gen_desktop.py /path/to/minimax-ai-code-0.7.8.tgz
gofmt -w desktop/zz_types_gen.go desktop/zz_methods_gen.go desktop/zz_catalog_gen.go
git diff --exit-code
```

Offline checks, with no CLI and no API key:

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

`MCODE_E2E=1` runs the no-key app-server tests. `MCODE_E2E_LLM=1` runs model scenarios and needs `MCODE_LLM_DATA_DIR`. GitHub Actions does not set `MCODE_E2E_LLM`.

A private acceptance of commit `d907d518f23d5778220906ded6daa83ea7cf8d07` recorded 305 accepted and 19 remaining. A newer commit is not that run. Read [the acceptance note](../acceptance.zh-CN.md) before you treat a method as checked.
