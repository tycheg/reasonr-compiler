# ReasonR for VS Code

Support for the [ReasonR](https://github.com/tycheg/reasonr-compiler) business logic language (`*.rr` files).

- **Highlighting**: the TextMate grammar works without the server too; with the server, semantic highlighting is added: fields, parameters, events, errors, notifications, public and private commands, personal data.
- **Diagnostics**: compiler errors and warnings as you type, with notes and declaration places.
- **Quick fixes**: compiler hints with edits ("add an index" and others).
- **Formatting**: the same as `reasonr fmt`; the **ReasonR: Format** command or regular document formatting.
- **Hover**: the type, the signature, the `##` documentation, the personal data subject.
- **Go to definition and find references**, extensions included.
- **Completion**: members after `.` and `?.`, argument names, enum values, fields in `where` (index fields first), keywords for the place.
- **ReasonR: Explain command**: a description of the method at the cursor: access, checks, actions, callers (`reasonr explain`).

## Installation

1. Build the compiler and put it on `PATH`:

   ```
   go install github.com/tycheg/reasonr-compiler/cmd/reasonr@latest
   ```

   or set its path in the `reasonr.server.path` setting.

2. Build and install the extension:

   ```
   cd vscode
   npm install
   npm run package
   code --install-extension reasonr-0.1.2.vsix
   ```

   VS Code does not reread a package with the same version until a full restart: after changing the extension, bump `version` in `package.json`.

## Settings

| Setting | Default | Description |
|---|---|---|
| `reasonr.server.path` | `reasonr` | The compiler path; the server starts with the command `reasonr lsp` |

## Development

- `npm run compile` builds `out/extension.js`;
- `npm test` checks the grammar on the RFC examples;
- F5 in VS Code with the `vscode` directory open runs the extension in a separate window.
