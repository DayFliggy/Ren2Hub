// diffLines from upstream components/source-diff.tsx, kept as a module so the
// component stays template-only.
export type DiffLine = {
  id: string
  kind: 'same' | 'added' | 'removed'
  text: string
}

export function diffLines(before: string, after: string): DiffLine[] {
  const left = before.split('\n')
  const right = after.split('\n')
  const lengths = Array.from({ length: left.length + 1 }, () =>
    Array<number>(right.length + 1).fill(0)
  )
  for (let i = left.length - 1; i >= 0; i -= 1) {
    for (let j = right.length - 1; j >= 0; j -= 1) {
      lengths[i]![j] =
        left[i] === right[j]
          ? lengths[i + 1]![j + 1]! + 1
          : Math.max(lengths[i + 1]![j]!, lengths[i]![j + 1]!)
    }
  }
  const result: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < left.length || j < right.length) {
    if (i < left.length && j < right.length && left[i] === right[j]) {
      result.push({ id: `same-${i}-${j}`, kind: 'same', text: left[i]! })
      i += 1
      j += 1
    } else if (
      j < right.length &&
      (i === left.length || lengths[i]![j + 1]! >= lengths[i + 1]![j]!)
    ) {
      result.push({ id: `added-${i}-${j}`, kind: 'added', text: right[j]! })
      j += 1
    } else {
      result.push({ id: `removed-${i}-${j}`, kind: 'removed', text: left[i]! })
      i += 1
    }
  }
  return result
}
