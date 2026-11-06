export function count(n: number, one: string, many: string = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`
}
