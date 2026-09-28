// Site names follow the daemon's rule (sites.NormalizeName): one DNS label
// of lowercase letters, digits and hyphens, served as <name>.test.

const NAME = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

/** suggestName turns a folder name into a valid site name:
 *  "Laravel App_v2" becomes "laravel-app-v2". */
export function suggestName(folder: string): string {
  return folder
    .toLowerCase()
    .replace(/\.test$/, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/** nameProblem says what's wrong with a typed site name, or null. */
export function nameProblem(name: string, taken: string[]): string | null {
  const n = name
    .trim()
    .toLowerCase()
    .replace(/\.test$/, "");
  if (n === "") return "Enter a name.";
  if (!NAME.test(n)) return "Use letters, digits and hyphens, not starting or ending with a hyphen.";
  if (taken.includes(n)) return `${n}.test already exists.`;
  return null;
}

/** folderName is the last segment of a path on any OS. */
export function folderName(path: string): string {
  return (
    path
      .replace(/[\\/]+$/, "")
      .split(/[\\/]/)
      .pop() ?? ""
  );
}

const ADJECTIVES = [
  "amber",
  "bold",
  "brave",
  "bright",
  "calm",
  "clever",
  "cosmic",
  "crisp",
  "daring",
  "eager",
  "fancy",
  "fuzzy",
  "gentle",
  "golden",
  "happy",
  "humble",
  "jolly",
  "lucky",
  "lunar",
  "mellow",
  "mighty",
  "misty",
  "nimble",
  "noble",
  "polar",
  "quiet",
  "rapid",
  "rustic",
  "silent",
  "snowy",
  "solar",
  "spicy",
  "stellar",
  "sunny",
  "swift",
  "tidy",
  "vivid",
  "wild",
  "witty",
  "zesty",
];
const NOUNS = [
  "badger",
  "beacon",
  "breeze",
  "canyon",
  "comet",
  "cedar",
  "coral",
  "falcon",
  "fjord",
  "forest",
  "harbor",
  "heron",
  "island",
  "lagoon",
  "lantern",
  "maple",
  "meadow",
  "meteor",
  "otter",
  "panda",
  "pebble",
  "phoenix",
  "pine",
  "prairie",
  "quartz",
  "raven",
  "reef",
  "river",
  "rocket",
  "sparrow",
  "summit",
  "tiger",
  "tulip",
  "valley",
  "walrus",
  "willow",
  "wombat",
  "yak",
  "zebra",
  "zephyr",
];

/** randomName suggests a fresh site name like "brave-otter", avoiding
 *  taken ones (a number is added in the unlikely case all tries clash). */
export function randomName(taken: string[], random: () => number = Math.random): string {
  const pick = (words: string[]) => words[Math.floor(random() * words.length)];
  for (let i = 0; i < 20; i++) {
    const name = `${pick(ADJECTIVES)}-${pick(NOUNS)}`;
    if (!taken.includes(name)) return name;
  }
  let n = 2;
  const base = `${pick(ADJECTIVES)}-${pick(NOUNS)}`;
  while (taken.includes(`${base}-${n}`)) n++;
  return `${base}-${n}`;
}
