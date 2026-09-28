// ui-lint enforces the mechanical rules of docs/ui-standard.md over app/src.
// Usage: bun scripts/ui-lint.ts [files...]   (no args: every file in src/)
// Exits 1 on any violation. A rule can be waived for one file with a line
// comment `// ui-standard-allow <RULE>: <reason of 10+ chars>`, which code
// review checks.
import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";

type Violation = { file: string; line: number; rule: string; message: string };
type Check = { rule: string; applies: (path: string) => boolean; run: (src: string, path: string) => Array<[number, string]> };

const APP = resolve(import.meta.dir, "..");
const SRC = join(APP, "src");

const isUi = (p: string) => p.includes(`${sep}components${sep}ui${sep}`);
const isTsx = (p: string) => p.endsWith(".tsx");
const isCode = (p: string) => /\.(tsx?|css)$/.test(p);

/** lineOf maps a string offset to its 1-based line number. */
function lineOf(src: string, offset: number): number {
  let line = 1;
  for (let i = 0; i < offset; i++) if (src.charCodeAt(i) === 10) line++;
  return line;
}

/** matches returns [line, text] for every regex hit. */
function matches(src: string, re: RegExp, message: (m: RegExpExecArray) => string): Array<[number, string]> {
  const out: Array<[number, string]> = [];
  const g = new RegExp(re.source, re.flags.includes("g") ? re.flags : re.flags + "g");
  for (let m = g.exec(src); m; m = g.exec(src)) out.push([lineOf(src, m.index), message(m)]);
  return out;
}

const PALETTE =
  "red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|slate|gray|zinc|neutral|stone";

// Endpoints whose effects are costly or hard to undo (C1).
const CONSEQUENTIAL = /\/api\/(binaries\/install|sites\/unlink|sites\/unpark|setup|shutdown)\b|"DELETE"/;
const DAEMON_CONSEQUENTIAL = /\{[^}]*\b(stop|runSetup)\b[^}]*\}\s*=\s*useDaemonContext\(\)/;

const CHECKS: Check[] = [
  {
    rule: "C1",
    applies: (p) => isTsx(p) && !isUi(p),
    run: (src) => {
      if (src.includes("<ConfirmAction")) return [];
      const hits = [
        ...matches(src, CONSEQUENTIAL, (m) => `${m[0]} is consequential: trigger it from a <ConfirmAction> dialog`),
        ...matches(src, DAEMON_CONSEQUENTIAL, (m) => `${m[1]}() is consequential: trigger it from a <ConfirmAction> dialog`),
      ];
      return hits;
    },
  },
  {
    rule: "F2",
    // lib/api.ts defines api(); every other caller wraps it.
    applies: (p) => /\.tsx?$/.test(p) && !isUi(p) && !p.endsWith(`${sep}lib${sep}api.ts`),
    run: (src) => {
      const out: Array<[number, string]> = [];
      const re = /\bapi(?:<[^>()]*>)?\(/g;
      for (let m = re.exec(src); m; m = re.exec(src)) {
        const before = src.slice(Math.max(0, m.index - 160), m.index);
        if (!before.includes("withTimeout(")) out.push([lineOf(src, m.index), "api() call without withTimeout(): pending states must resolve"]);
      }
      return out;
    },
  },
  {
    rule: "F6",
    applies: (p) => /\.tsx?$/.test(p) && !p.endsWith(`${sep}lib${sep}open.ts`),
    run: (src) => matches(src, /\bvoid\s+(openUrl|revealItemInDir)\(/, (m) => `void ${m[1]}() hides failures: use openExternal()/revealPath() from lib/open.ts`),
  },
  {
    rule: "V1",
    applies: (p) => isTsx(p) && !isUi(p),
    run: (src) => [
      ...matches(src, /(?<![&\w/])#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b(?=["'`\s;,)\]])/, (m) => `raw color ${m[0]}: use a token from index.css`),
      ...matches(src, new RegExp(`\\b(?:bg|text|border|ring|fill|stroke|from|via|to|outline|decoration|divide|placeholder)-(?:${PALETTE})-\\d{2,3}\\b`), (m) => `palette color ${m[0]}: use a semantic token`),
    ],
  },
  {
    rule: "V4",
    applies: (p) => isTsx(p) && !isUi(p),
    run: (src) => matches(src, /\bspace-[xy]-\d/, (m) => `${m[0]}: use flex/grid with gap-*`),
  },
  {
    rule: "V5",
    applies: (p) => /\.tsx?$/.test(p),
    run: (src) => [
      ...matches(src, /from\s+["']lucide-react["']/, () => "lucide-react: icons are Phosphor (@phosphor-icons/react)"),
      ...matches(src, /\bShield\w*Icon\b/, (m) => `${m[0]}: shield icons are banned`),
    ],
  },
  {
    rule: "W3",
    applies: isCode,
    run: (src) => matches(src, /\u2014/, () => "em-dash: rewrite with a comma, colon or parentheses"),
  },
  {
    rule: "W4",
    applies: (p) => /\.tsx?$/.test(p),
    run: (src) => matches(src, /(?<![\w.])(?:window\.)?(alert|confirm|prompt)\(/, (m) => `native ${m[1]}(): use ConfirmAction or an inline message`),
  },
];

/** waived returns the rules a file opts out of, each with a reason. */
function waived(src: string): Set<string> {
  const out = new Set<string>();
  for (const m of src.matchAll(/ui-standard-allow\s+([A-Z]\d+):\s*(.{10,})/g)) out.add(m[1]);
  return out;
}

function walk(dir: string): string[] {
  return readdirSync(dir, { recursive: true, withFileTypes: true })
    .filter((d) => d.isFile())
    .map((d) => join(d.parentPath, d.name));
}

export function lintFile(path: string): Violation[] {
  const src = readFileSync(path, "utf8");
  const skip = waived(src);
  const out: Violation[] = [];
  for (const c of CHECKS) {
    if (skip.has(c.rule) || !c.applies(path)) continue;
    for (const [line, message] of c.run(src, path)) out.push({ file: relative(APP, path), line, rule: c.rule, message });
  }
  return out;
}

if (import.meta.main) {
  const args = process.argv.slice(2).map((a) => resolve(a));
  const files = (args.length ? args : walk(SRC)).filter((p) => p.startsWith(SRC) && isCode(p));
  const violations = files.flatMap(lintFile);
  for (const v of violations) console.error(`${v.file}:${v.line}: [${v.rule}] ${v.message}`);
  if (violations.length) {
    console.error(`\n${violations.length} UI standard violation(s). Rules: docs/ui-standard.md`);
    process.exit(1);
  }
  console.log(`ui-lint: ${files.length} file(s) clean`);
}
