/** @type {import('tailwindcss').Config} */
// Токены темы — docs/design.md. Hex только здесь.
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: '#2d3137',
        'ink-muted': '#868686',
        brand: '#0e41d2',
        'brand-link': '#1948c4',
        'brand-tint': 'rgba(14,65,210,0.1)',
        'brand-soft': '#dfe7ff',
        surface: '#ffffff',
        'surface-muted': '#f4f4f4',
        line: '#e5e5e5',
        ok: '#008900',
        warn: '#b25e00',
        'warn-bg': '#fff4e5',
        bad: '#c62828',
        'bad-bg': '#fdecec',
      },
      fontFamily: {
        sans: ['"PT Root UI"', '"Helvetica Neue"', 'Helvetica', 'Arial', 'system-ui', 'sans-serif'],
      },
      borderRadius: {
        card: '16px',
        inner: '12px',
        sm: '8px',
        xs: '4px',
      },
      backgroundImage: {
        'grad-brand': 'linear-gradient(135deg, #0460ea 20%, #4225f2 87%)',
        'grad-promo': 'linear-gradient(127deg, #c7fb94 22%, #a3f396 87%)',
      },
      fontSize: {
        price: ['20px', { lineHeight: '24px', fontWeight: '700' }],
        title: ['16px', { lineHeight: '22px', fontWeight: '700' }],
        button: ['16px', { lineHeight: '20px', fontWeight: '500' }],
        body: ['14px', { lineHeight: '19px', fontWeight: '400' }],
        caption: ['12px', { lineHeight: '16px', fontWeight: '400' }],
      },
    },
  },
  plugins: [],
}
