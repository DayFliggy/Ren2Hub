import { parse, type Node } from 'acorn'
import type { TaskPluginPreview } from '../types'
import { computeSourceSha256, MAX_PLUGIN_SOURCE_BYTES } from './plugin-url'

type SyntaxNode = Node & Record<string, unknown>
const unresolved = Symbol('dynamic metadata')

function node(value: unknown): value is SyntaxNode {
  return Boolean(value && typeof value === 'object' && 'type' in value)
}

function literal(value: unknown, depth = 0): unknown {
  if (!node(value) || depth > 32) return unresolved
  if (value.type === 'Literal') {
    const result = value.value
    return result === null ||
      typeof result === 'string' ||
      typeof result === 'boolean' ||
      (typeof result === 'number' && Number.isFinite(result))
      ? result
      : unresolved
  }
  if (value.type === 'UnaryExpression' && value.operator === '-') {
    const result = literal(value.argument, depth + 1)
    return typeof result === 'number' ? -result : unresolved
  }
  if (value.type === 'ArrayExpression' && Array.isArray(value.elements)) {
    const result = value.elements.map((item) => literal(item, depth + 1))
    return result.includes(unresolved) ? unresolved : result
  }
  if (value.type !== 'ObjectExpression' || !Array.isArray(value.properties))
    return unresolved
  const result: Record<string, unknown> = Object.create(null)
  for (const property of value.properties) {
    const key = propertyKey(property)
    if (key === null || Object.hasOwn(result, key)) return unresolved
    const item = literal(property.value, depth + 1)
    if (item === unresolved) return unresolved
    result[key] = item
  }
  return result
}

function propertyKey(property: SyntaxNode): string | null {
  if (
    property.type !== 'Property' ||
    property.computed ||
    property.method ||
    property.kind !== 'init' ||
    !node(property.key)
  )
    return null
  if (property.key.type === 'Identifier') return String(property.key.name)
  return property.key.type === 'Literal' &&
    typeof property.key.value === 'string'
    ? property.key.value
    : null
}

/** Only literal metadata is previewed. Source code is never executed in the browser. */
export async function previewPluginSource(
  source: string,
  signal?: AbortSignal
): Promise<TaskPluginPreview> {
  signal?.throwIfAborted()
  const result: TaskPluginPreview = {
    meta: {},
    unresolved: ['meta'],
    source_hash: '',
  }
  if (new TextEncoder().encode(source).byteLength > MAX_PLUGIN_SOURCE_BYTES)
    return result
  let tree: SyntaxNode
  try {
    tree = parse(source, {
      ecmaVersion: 'latest',
      sourceType: 'module',
    }) as unknown as SyntaxNode
  } catch {
    // Syntax errors are displayed as unavailable; upload validation remains authoritative.
    return result
  }
  const declarations: SyntaxNode[] = []
  const identifiers: SyntaxNode[] = []
  let tooDeep = false
  const visit = (value: unknown, depth = 0): void => {
    if (!node(value)) return
    if (depth > 128) {
      tooDeep = true
      return
    }
    if (value.type === 'Identifier' && value.name === 'meta')
      identifiers.push(value)
    if (
      value.type === 'VariableDeclarator' &&
      node(value.id) &&
      value.id.name === 'meta'
    )
      declarations.push(value)
    for (const child of Object.values(value)) {
      if (Array.isArray(child)) child.forEach((item) => visit(item, depth + 1))
      else if (node(child)) visit(child, depth + 1)
    }
  }
  visit(tree)
  const declaration = declarations[0]
  const topLevel =
    Array.isArray(tree.body) &&
    tree.body.some((statement: SyntaxNode) => {
      const item =
        statement.type === 'ExportNamedDeclaration'
          ? statement.declaration
          : statement
      return (
        node(item) &&
        item.type === 'VariableDeclaration' &&
        Array.isArray(item.declarations) &&
        item.declarations.includes(declaration)
      )
    })
  if (
    tooDeep ||
    !topLevel ||
    declarations.length !== 1 ||
    identifiers.length !== 1 ||
    !declaration ||
    !node(declaration.init) ||
    declaration.init.type !== 'ObjectExpression' ||
    !Array.isArray(declaration.init.properties)
  )
    return result
  // Mutation, aliasing, spreads and computed keys make absence impossible to prove.
  const properties = declaration.init.properties as SyntaxNode[]
  const keys = properties.map(propertyKey)
  if (keys.includes(null) || new Set(keys).size !== keys.length) return result
  result.unresolved = []
  result.meta = Object.create(null)
  for (const [index, property] of properties.entries()) {
    const key = keys[index] as string
    const value = literal(property.value)
    if (value === unresolved) result.unresolved.push(key)
    else result.meta[key] = value
  }
  result.source_hash = (await computeSourceSha256(source)) ?? ''
  signal?.throwIfAborted()
  return result
}
