const themedColor = (token) => `rgb(var(--${token}) / <alpha-value>)`

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Existing utilities resolve through the active semantic theme. This
        // keeps component structure stable while light/dark palettes change.
        white: themedColor('color-white'),
        black: themedColor('color-black'),
        brand: {
          50: themedColor('color-brand-50'),
          100: themedColor('color-brand-100'),
          200: themedColor('color-brand-200'),
          300: themedColor('color-brand-300'),
          400: themedColor('color-brand-400'),
          500: themedColor('color-brand-500'),
          600: themedColor('color-brand-600'),
          700: themedColor('color-brand-700'),
          800: themedColor('color-brand-800'),
          900: themedColor('color-brand-900'),
        },
        honey: {
          50: themedColor('color-accent-50'),
          100: themedColor('color-accent-100'),
          200: themedColor('color-accent-200'),
          300: themedColor('color-accent-300'),
          400: themedColor('color-accent-400'),
          500: themedColor('color-accent-500'),
          600: themedColor('color-accent-600'),
          700: themedColor('color-accent-700'),
          800: themedColor('color-accent-800'),
          900: themedColor('color-accent-900'),
        },
      },
      fontFamily: {
        sans: [
          'Ren2Inter',
          'Ren2NotoSansSC',
          'Inter',
          'system-ui',
          '-apple-system',
          'Segoe UI',
          'Roboto',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif',
        ],
        mono: [
          'Ren2JetBrainsMono',
          'JetBrains Mono',
          'IBM Plex Mono',
          'ui-monospace',
          'SFMono-Regular',
          'Menlo',
          'monospace',
        ],
        serif: [
          'Ren2NotoSerifSC',
          'Noto Serif SC',
          'Songti SC',
          'SimSun',
          'serif',
        ],
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'fade-in': 'fadeIn 0.6s ease-out both',
        'rise-in': 'riseIn 0.7s cubic-bezier(0.2, 0.6, 0.2, 1) both',
        'scale-in': 'scaleIn 0.24s cubic-bezier(0.2, 0.6, 0.2, 1) both',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' },
        },
        riseIn: {
          '0%': { opacity: '0', transform: 'translateY(20px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.96) translateY(8px)' },
          '100%': { opacity: '1', transform: 'scale(1) translateY(0)' },
        },
      },
      backdropBlur: {
        xs: '2px',
      },
      transitionTimingFunction: {
        'spring-snappy': 'cubic-bezier(0.16, 1, 0.3, 1)',
        'spring-smooth': 'cubic-bezier(0.25, 1, 0.5, 1)',
        'spring-bouncy': 'cubic-bezier(0.34, 1.56, 0.64, 1)',
      },
    },
  },
  plugins: [],
}
