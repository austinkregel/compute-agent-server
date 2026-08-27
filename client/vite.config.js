import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
// https://vite.dev/config/
import proxy from './proxy.config.js';

process.env.VITE_GITHUB_USERNAME = 'austinkregel';

export default defineConfig({
  plugins: [tailwindcss(), vue()],
  server: {
    proxy,
  },
})
