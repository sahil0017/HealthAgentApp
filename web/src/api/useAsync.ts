import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "./client";

export interface Async<T> { data: T | null; error: string | null; loading: boolean; reload: () => void }

export function messageOf(e: unknown): string {
  return e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Something went wrong";
}

export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): Async<T> {
  const [state, setState] = useState<{ data: T | null; error: string | null; loading: boolean }>({ data: null, error: null, loading: true });
  const [tick, setTick] = useState(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  useEffect(() => {
    let live = true;
    setState((s) => ({ ...s, loading: true, error: null }));
    fnRef.current().then(
      (data) => live && setState({ data, error: null, loading: false }),
      (e) => live && setState({ data: null, error: messageOf(e), loading: false }),
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);
  const reload = useCallback(() => setTick((t) => t + 1), []);
  return { ...state, reload };
}
