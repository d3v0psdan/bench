/** Wire types for log endpoints (daemon/internal/api LogTail, LogChunk). */
export interface LogTail {
  path: string;
  text: string;
  /** File length when read; the follow socket resumes from here. */
  size: number;
}

export interface LogChunk {
  text: string;
  /** The file shrank (rotated or truncated): drop what is shown first. */
  reset?: boolean;
}

/** The most lines a log view keeps; older ones scroll away. */
export const MAX_LOG_LINES = 5000;

/** appendLog adds a follow chunk to the shown text, keeping only the last
 *  maxLines lines so a chatty process can't grow the view without bound. */
export function appendLog(shown: string, chunk: LogChunk, maxLines = MAX_LOG_LINES): string {
  const text = (chunk.reset ? "" : shown) + chunk.text;
  let cut = text.length;
  // Walk back past maxLines newlines; a trailing newline ends a line
  // rather than starting one.
  let seen = text.endsWith("\n") ? -1 : 0;
  while (cut > 0) {
    const i = text.lastIndexOf("\n", cut - 1);
    if (i < 0) return text;
    if (++seen === maxLines) return text.slice(i + 1);
    cut = i;
  }
  return text;
}
