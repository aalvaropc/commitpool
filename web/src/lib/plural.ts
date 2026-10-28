/** "1 paid seat", "3 paid seats", "2 people": a count with its noun in the right number. */
export function count(n: number, one: string, many: string = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`
}
