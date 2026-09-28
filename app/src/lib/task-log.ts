// A task log (daemon internal/newapp) is plain text: "== <step>" starts
// each step, "$ <command>" precedes each command's output. Tools draw
// colours and spinners with terminal codes, which are cleaned out here.

export interface LogSection {
  title: string;
  lines: string[];
}

// Colour and cursor sequences (ESC [ ... letter).
const TERMINAL_CODES = new RegExp(String.fromCharCode(27) + "\[[0-9;?]*[A-Za-z]", "g");

/** cleanLog drops terminal codes and keeps only each spinner line's final
 *  redraw (the text after its last carriage return). */
export function cleanLog(text: string): string {
  return text
    .replace(TERMINAL_CODES, "")
    .split("\n")
    .map((line) => {
      const trimmed = line.replace(/\r+$/, "");
      const i = trimmed.lastIndexOf("\r");
      return (i >= 0 ? trimmed.slice(i + 1) : trimmed).trimEnd();
    })
    .join("\n");
}

/** splitSteps groups a cleaned log into its steps, dropping blank lines. */
export function splitSteps(text: string): LogSection[] {
  const sections: LogSection[] = [];
  for (const line of cleanLog(text).split("\n")) {
    if (line.startsWith("== ")) {
      sections.push({ title: line.slice(3).trim(), lines: [] });
    } else if (line.trim() !== "") {
      if (sections.length === 0) sections.push({ title: "Starting", lines: [] });
      sections[sections.length - 1].lines.push(line);
    }
  }
  return sections;
}
