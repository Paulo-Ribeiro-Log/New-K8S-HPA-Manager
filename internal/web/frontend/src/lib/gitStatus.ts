// Apresentação do status do `git status --porcelain` (código XY de duas letras) nas telas do
// Code Editor: letra do selo e cor.

/** Letra do selo: U (não rastreado), A, D, R, C ou M. */
export function scmBadge(xy: string): string {
  if (xy === "??" || xy.startsWith("?")) return "U";
  const x = xy[0], y = xy[1];
  if (x === "A") return "A";
  if (x === "D" || y === "D") return "D";
  if (x === "R") return "R";
  if (x === "C") return "C";
  return "M";
}

export function scmColor(xy: string): string {
  if (xy === "??" || xy.startsWith("?")) return "text-green-400"; // untracked
  if (xy.includes("A")) return "text-green-400";                   // added
  if (xy.includes("D")) return "text-red-400";                     // deleted
  if (xy.startsWith("R")) return "text-orange-400";                // renamed
  return "text-yellow-400";                                        // modified
}

/** Arquivo novo (não rastreado)? */
export function isUntracked(xy: string): boolean {
  return xy === "??" || xy === "? ";
}

/** Já está no índice (staging)? */
export function isStaged(xy: string): boolean {
  return xy[0] !== " " && xy[0] !== "?";
}
