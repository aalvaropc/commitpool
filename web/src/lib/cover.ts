export interface Ring {
  cx: number
  cy: number
  r: number
  width: number
}

export interface Cover {
  rings: Ring[]
}

function seeded(seed: string): () => number {
  let state = 2166136261
  for (const ch of seed) {
    state ^= ch.codePointAt(0) ?? 0
    state = Math.imul(state, 16777619)
  }
  return () => {
    state = Math.imul(state ^ (state >>> 15), 2246822507)
    state = Math.imul(state ^ (state >>> 13), 3266489909)
    state ^= state >>> 16
    return (state >>> 0) / 4294967296
  }
}

export function cover(id: string): Cover {
  const next = seeded(id || 'commitpool')
  const between = (min: number, max: number) => Math.round(min + next() * (max - min))
  return {
    rings: [
      { cx: between(78, 100), cy: between(0, 22), r: between(30, 42), width: between(9, 14) },
      { cx: between(0, 30), cy: between(80, 100), r: between(16, 26), width: between(5, 8) },
    ],
  }
}
