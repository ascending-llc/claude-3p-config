# ASCENDING Claude Desktop Employee Deployment Bundle

This repository contains the employee deployment templates, Go credential-process source, and Python packaging script. It produces self-service installation directories for macOS (Apple Silicon and Intel) and Windows x64. The build does not depend on a neighboring reference repository.

After downloading the repository, use [`config.json.example`](deployment/src/config.example/config.json.example) to create a private configuration at `deployment/src/config/config.json`. The build machine needs Python 3.9+ and Go 1.24+. Building the macOS Intel binary from source also requires a local C toolchain that supports that architecture. From the repository root, run:

```bash
python3 scripts/build_employee_dist.py
```

`--config` reads the private file above by default. The Go source is in this repository at `source/go`, and the packager builds the three credential-process binaries from it by default. The first build may download dependencies declared in `go.mod`. Use `--go-source <path>` only to override the source location. If you already have trusted binaries, use `--binaries <directory>`; see the required filenames in the [packaging guide](deployment/src/README-packaging.md).

The output is written to `dist/macos/` and `dist/windows/`. Git ignores both `dist/` and the private configuration directory; do not commit or distribute them publicly. Employees should follow the README in their platform's output directory to install and sign in.

Validation command: `python3 -m unittest discover -s tests -v`.

Upstream project: [guidance-for-claude-code-with-amazon-bedrock](https://github.com/aws-solutions-library-samples/guidance-for-claude-code-with-amazon-bedrock)
