/** Each downloaded file holds at most this many UTF-8 bytes (plus one row). */
export const EXPORT_VOLUME_BYTES = 32 * 1024 * 1024

/** Text that makes one complete file around serialized rows. */
export interface ExportVolumeFormat {
  prefix: string
  suffix: string
  /** Inserted between two rows of the same file. */
  separator: string
  mime: string
}

export interface ExportVolumes {
  append(row: string): void
  /** Saves the last file and returns how many files were saved. */
  finish(): number
  /** Files already handed to `save`. */
  readonly saved: number
}

function utf8Length(text: string): number {
  let bytes = 0
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index)
    if (code < 0x80) bytes += 1
    else if (code < 0x800) bytes += 2
    else if (code >= 0xd800 && code <= 0xdbff) {
      bytes += 4
      index++
    } else bytes += 3
  }
  return bytes
}

/**
 * Splits a streamed export into complete files of about `volumeBytes`. A full
 * file is saved only when the next row arrives, so a single file keeps its
 * plain name and only real multi-file exports are numbered (part 1, 2, …).
 * At most one file plus one row is held in memory.
 */
export function createExportVolumes(
  format: ExportVolumeFormat,
  save: (blob: Blob, part: number | null) => void,
  volumeBytes = EXPORT_VOLUME_BYTES
): ExportVolumes {
  const frameBytes = utf8Length(format.prefix) + utf8Length(format.suffix)
  const separatorBytes = utf8Length(format.separator)
  let parts: string[] = []
  let bytes = frameBytes
  let saved = 0

  const flush = (part: number | null) => {
    const blob = new Blob([format.prefix, ...parts, format.suffix], {
      type: format.mime,
    })
    parts = []
    bytes = frameBytes
    saved++
    save(blob, part)
  }

  return {
    append(row) {
      const rowBytes = utf8Length(row)
      if (parts.length > 0 && bytes + separatorBytes + rowBytes > volumeBytes)
        flush(saved + 1)
      if (parts.length > 0) {
        parts.push(format.separator)
        bytes += separatorBytes
      }
      parts.push(row)
      bytes += rowBytes
    },
    finish() {
      flush(saved === 0 ? null : saved + 1)
      return saved
    },
    get saved() {
      return saved
    },
  }
}
