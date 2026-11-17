// 字符雨纹理层：压暗的 API 日志风格字符流，垫在地图与节点之间，
// 为点阵世界地图补一层"网关实时处理"的技术氛围。保留自参考站(Midjourney)的 ASCII 语言。
import { getCanvasTheme, withAlpha, type CanvasTheme } from './theme'

const CHARS = 'POST/v1/chat/completions{model:stream}route:healthy 01→←⟶✓·'
const FONT = 15

export class CharRain {
  private ctx: CanvasRenderingContext2D
  private h = 0
  private cols = 0
  private drops: number[] = []
  private speeds: number[] = []
  reduced = false

  constructor(ctx: CanvasRenderingContext2D, reduced = false) {
    this.ctx = ctx
    this.reduced = reduced
  }

  setLayout(w: number, h: number) {
    this.h = h
    this.cols = Math.ceil(w / FONT)
    this.drops = new Array(this.cols)
    this.speeds = new Array(this.cols)
    for (let i = 0; i < this.cols; i++) {
      this.drops[i] = Math.random() * -60
      this.speeds[i] = 0.16 + Math.random() * 0.34
    }
  }

  // 隔列稀疏：日间每 2 列画 1 列，夜间每 4 列画 1 列；其余列既不画也不推进。
  private static drawsColumn(column: number, theme: CanvasTheme): boolean {
    return column % (theme.name === 'dark' ? 4 : 2) === 1
  }

  /** 字符下落一个固定步长（60Hz）；落出画面后随机回到顶部。 */
  update(theme: CanvasTheme) {
    if (this.reduced) return
    for (let i = 0; i < this.cols; i++) {
      if (!CharRain.drawsColumn(i, theme)) continue
      if (this.drops[i] * FONT > this.h && Math.random() > 0.975)
        this.drops[i] = Math.random() * -20
      else this.drops[i] += this.speeds[i]
    }
  }

  draw(theme: CanvasTheme = getCanvasTheme('dark')) {
    if (this.reduced) return // 降级：不绘制字符雨
    const ctx = this.ctx
    ctx.font = `${FONT}px 'JetBrains Mono', monospace`
    ctx.textAlign = 'left'
    ctx.textBaseline = 'top'
    for (let i = 0; i < this.cols; i++) {
      if (!CharRain.drawsColumn(i, theme)) continue
      const rowY = this.drops[i] * FONT
      const ch = CHARS[Math.floor(Math.random() * CHARS.length)]
      const x = i * FONT
      // 主体极暗绿（大幅下调透明度作垫底氛围）
      ctx.fillStyle = withAlpha(
        theme.charRain,
        theme.name === 'light' ? 0.055 : 0.04
      )
      ctx.fillText(ch, x, rowY)
      // 前导字符略亮
      ctx.fillStyle = withAlpha(
        theme.charRainLead,
        theme.name === 'light' ? 0.08 : 0.065
      )
      ctx.fillText(ch, x, rowY)
    }
  }
}
