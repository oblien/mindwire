import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  return {
    server: {
      host: '127.0.0.1',
      proxy: {
        '/mindwire': {
          target: env.MINDWIRE_URL || 'http://127.0.0.1:8790',
          changeOrigin: true,
          rewrite: path => path.replace(/^\/mindwire/, ''),
          headers: env.MINDWIRE_TOKEN ? { Authorization: `Bearer ${env.MINDWIRE_TOKEN}` } : {},
        },
      },
    },
  };
});
