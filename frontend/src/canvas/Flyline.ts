import { arcAway, type Pt } from './arc'
import { getCanvasTheme, type CanvasTheme } from './theme'

const FLOW_CHARS = [
  '0',
  '1',
  '{',
  '}',
  '<',
  '>',
  '/',
  '·',
  '=',
  ':',
  'x',
  '+',
  'tok',
  'API',
  'LLM',
]

/** The model end of a flyline, read on every draw. */
interface FlylineModel {
  readonly color: string
  readonly visible: boolean
  readonly edgeAlpha: number
}

/** Gateway ↔ model packet, advanced by duration rather than frame count.
 *  `from` and `to` are the live hub and marker: the map rotates and relayouts
 *  move them mid-flight, so the arc is derived from their current positions.
 *  Like the hub link, it is hidden and faded with its model marker, which
 *  also hides the jump when the marker wraps across the map seam. */
export class Flyline {
  progress = 0
  dead = false
  private chars: string[] = []
  private static readonly TAIL = 9

  constructor(
    public from: Pt,
    public to: Pt,
    private model: FlylineModel,
    private durationMs: number,
    public onArrive: () => void,
    private getCenter: () => Pt
  ) {
    for (let i = 0; i < Flyline.TAIL; i++) {
      this.chars.push(FLOW_CHARS[Math.floor(Math.random() * FLOW_CHARS.length)])
    }
  }

  private control(): Pt {
    return arcAway(this.from, this.to, this.getCenter(), 0.2)
  }

  point(t: number, control: Pt = this.control()): Pt {
    const u = 1 - t
    return {
      x: u * u * this.from.x + 2 * u * t * control.x + t * t * this.to.x,
      y: u * u * this.from.y + 2 * u * t * control.y + t * t * this.to.y,
    }
  }

  update(deltaMs: number) {
    if (this.dead) return
    this.progress = Math.min(
      1,
      this.progress + Math.max(0, deltaMs) / Math.max(1, this.durationMs)
    )
    if (this.progress >= 1) {
      this.dead = true
      this.onArrive()
    }
  }

  draw(
    ctx: CanvasRenderingContext2D,
    t: number,
    reduced = false,
    theme: CanvasTheme = getCanvasTheme('dark')
  ) {
    const { color, visible, edgeAlpha } = this.model
    if (!visible || edgeAlpha <= 0) return
    const control = this.control()
    ctx.save()
    ctx.globalAlpha = edgeAlpha
    ctx.save()
    ctx.beginPath()
    ctx.moveTo(this.from.x, this.from.y)
    ctx.quadraticCurveTo(control.x, control.y, this.to.x, this.to.y)
    ctx.strokeStyle = color
    ctx.globalAlpha = 0.22 * edgeAlpha
    ctx.lineWidth = 1
    ctx.setLineDash([4, 7])
    ctx.lineDashOffset = reduced ? 0 : -t * 0.8
    ctx.stroke()
    ctx.restore()

    if (!reduced) {
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      const step = 0.045
      for (let i = 0; i < Flyline.TAIL; i++) {
        const tailProgress = this.progress - i * step
        if (tailProgress < 0) break
        const p = this.point(tailProgress, control)
        const strength = 1 - i / Flyline.TAIL
        ctx.font = `${7 + strength * 5}px 'JetBrains Mono', monospace`
        ctx.fillStyle = this.rgba(color, 0.15 + strength * 0.65)
        ctx.fillText(this.chars[i], p.x, p.y)
      }
    }

    const head = this.point(this.progress, control)
    const glow = ctx.createRadialGradient(head.x, head.y, 0, head.x, head.y, 9)
    glow.addColorStop(0, color)
    glow.addColorStop(1, 'transparent')
    ctx.fillStyle = glow
    ctx.beginPath()
    ctx.arc(head.x, head.y, 9, 0, Math.PI * 2)
    ctx.fill()
    ctx.fillStyle = theme.packetCore
    ctx.beginPath()
    ctx.arc(head.x, head.y, 1.8, 0, Math.PI * 2)
    ctx.fill()
    ctx.restore()
  }

  private rgba(hex: string, alpha: number): string {
    const value = hex.replace('#', '')
    if (value.length < 6) return hex
    const red = parseInt(value.slice(0, 2), 16)
    const green = parseInt(value.slice(2, 4), 16)
    const blue = parseInt(value.slice(4, 6), 16)
    return `rgba(${red},${green},${blue},${alpha})`
  }
}
