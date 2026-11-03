import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { get, post } from "./client";
import type { Profile } from "./types";

interface Ctx {
  profiles: Profile[];
  profile: Profile | null;
  select: (id: string) => void;
  create: (name: string) => Promise<void>;
  refresh: () => Promise<void>;
  loading: boolean;
}

const ProfileContext = createContext<Ctx | null>(null);
const KEY = "cortex.profile";

export function ProfileProvider({ children }: { children: ReactNode }) {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [selected, setSelected] = useState<string | null>(sessionStorage.getItem(KEY));
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    const list = await get<Profile[]>("/profiles");
    setProfiles(list);
    setSelected((cur) => (cur && list.some((p) => p.id === cur) ? cur : list[0]?.id ?? null));
    setLoading(false);
  }, []);

  useEffect(() => {
    refresh().catch(() => setLoading(false));
  }, [refresh]);

  const value = useMemo<Ctx>(
    () => ({
      profiles,
      profile: profiles.find((p) => p.id === selected) ?? null,
      select: (id) => {
        sessionStorage.setItem(KEY, id);
        setSelected(id);
      },
      create: async (name) => {
        const p = await post<Profile>("/profiles", { display_name: name });
        sessionStorage.setItem(KEY, p.id);
        setSelected(p.id);
        await refresh();
      },
      refresh,
      loading,
    }),
    [profiles, selected, refresh, loading],
  );
  return <ProfileContext.Provider value={value}>{children}</ProfileContext.Provider>;
}

export function useProfile(): Ctx {
  const c = useContext(ProfileContext);
  if (!c) throw new Error("useProfile outside ProfileProvider");
  return c;
}
