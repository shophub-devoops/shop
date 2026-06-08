/** @type {import('tailwindcss').Config} */
// Shared design language with the ShopHub frontend (Railway-style dark theme).
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: '#0D0C14', // page background
        surface: '#13111C', // sunken surfaces (inputs, code)
        card: '#181622', // cards / panels
        elevated: '#1C1A28', // metric panels
        line: '#33323E', // borders
        fg: '#F7F7F8', // primary text
        muted: '#868593', // secondary text
        faint: '#535260', // tertiary text / icons
        accent: '#853BCE', // primary purple
        'accent-bright': '#A667E4', // hover purple
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', '-apple-system', 'sans-serif'],
        serif: ['"IBM Plex Serif"', 'Georgia', 'serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
      borderRadius: { lg: '8px', xl: '10px', '2xl': '12px' },
      letterSpacing: { tightest: '-0.04em' },
      keyframes: {
        'fade-up': {
          '0%': { opacity: '0', transform: 'translateY(16px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        'fade-in': { '0%': { opacity: '0' }, '100%': { opacity: '1' } },
      },
      animation: {
        'fade-up': 'fade-up 0.7s cubic-bezier(0.215,0.61,0.355,1) both',
        'fade-in': 'fade-in 1s ease-out both',
      },
    },
  },
  plugins: [],
};
