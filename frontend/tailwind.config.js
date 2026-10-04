/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
      colors: {
        ink: {
          950: '#0b0d12',
          900: '#11141b',
          800: '#171a23',
          700: '#232733',
          500: '#5b6478',
          300: '#a8b1c3',
          100: '#e7ebf2',
        },
        // Hawk gold, the color of Falconia's banners.
        accent: {
          600: '#b8901f',
          500: '#d4a93a',
          400: '#e0bc5c',
          300: '#ebd08a',
        },
      },
    },
  },
  plugins: [],
}
