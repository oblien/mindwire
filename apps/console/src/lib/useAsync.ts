// A tiny fetch-on-mount hook used by every config panel: it runs an async loader, tracks
// loading/error, exposes a `reload`, and drops results from a stale run when deps change or the
// component unmounts. Keeps the panels declarative without pulling in a data-fetching library.
import { useCallback, useEffect, useRef, useState } from "react";

export interface Async<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

export function useAsync<T>(loader: () => Promise<T>, deps: unknown[] = []): Async<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [nonce, setNonce] = useState(0);
  const latest = useRef(0);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  useEffect(() => {
    const ticket = ++latest.current;
    setLoading(true);
    setError(null);
    loader()
      .then((d) => {
        if (ticket === latest.current) setData(d);
      })
      .catch((e: unknown) => {
        if (ticket === latest.current) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (ticket === latest.current) setLoading(false);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nonce, ...deps]);

  return { data, error, loading, reload };
}
