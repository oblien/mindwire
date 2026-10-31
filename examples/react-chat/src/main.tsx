import { useEffect, useMemo } from 'react';
import { createRoot } from 'react-dom/client';
import { ChatProvider, DefaultPanel } from '@raptor-ai/react';
import { createMindwireAdapter } from '@raptor-ai/react/mindwire';
import '@raptor-ai/react/ui.css';
import './style.css';

function App() {
  const chatId = new URLSearchParams(location.search).get('chat') ?? 'react-example';
  const adapter = useMemo(() => createMindwireAdapter({
    baseUrl: '/mindwire', chatId,
    agent: import.meta.env.VITE_MINDWIRE_AGENT || 'codex',
    cwd: import.meta.env.VITE_MINDWIRE_CWD || undefined,
  }), [chatId]);
  useEffect(() => () => adapter.dispose(), [adapter]);
  return (
    <main>
      <ChatProvider autoLoadHistory key={chatId} apiHandler={adapter} authConfig={{ sessionId: chatId }}
        initialWelcome={{ title: 'Mindwire', description: 'What would you like to work on?' }}>
        <DefaultPanel inputProps={{ enableLive: false }} />
      </ChatProvider>
    </main>
  );
}

createRoot(document.getElementById('root')!).render(<App />);
