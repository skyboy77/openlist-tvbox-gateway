import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import ts from "typescript";

const input = resolve("tvbox/src/index.ts");
const output = resolve("internal/gateway/assets/openlist-tvbox.js");
const source = readFileSync(input, "utf8");

if (/^\s*import\s/m.test(source) || /^\s*export\s+(?:\*|\{).*from\s/m.test(source)) {
  throw new Error("tvbox spider build does not bundle imports; keep tvbox/src/index.ts self-contained");
}

const result = ts.transpileModule(source, {
  fileName: input,
  reportDiagnostics: true,
  compilerOptions: {
    target: ts.ScriptTarget.ES2020,
    module: ts.ModuleKind.ESNext,
    strict: true,
    removeComments: false,
  },
});

const diagnostics = result.diagnostics || [];
if (diagnostics.length > 0) {
  const message = ts.formatDiagnosticsWithColorAndContext(diagnostics, {
    getCanonicalFileName: (fileName) => fileName,
    getCurrentDirectory: () => process.cwd(),
    getNewLine: () => "\n",
  });
  throw new Error(message);
}

mkdirSync(dirname(output), { recursive: true });
writeFileSync(output, result.outputText, "utf8");
