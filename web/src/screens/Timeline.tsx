import { get } from "../api/client";
import { useProfile } from "../api/profile";
import type { TimelineItem } from "../api/types";
import { useAsync } from "../api/useAsync";
import { Badge, Card, Empty, Load, Page, fmtDate } from "../components/ui";

export function Timeline() {
  const { profile } = useProfile();
  const id = profile!.id;
  const q = useAsync(() => get<TimelineItem[]>(`/profiles/${id}/timeline`), [id]);
  return (
    <Page title="Timeline" subtitle="Everything in your record, newest first">
      <Load q={q}>
        {(items) => (
          <Card>
            {items.length === 0 && <Empty>Your timeline fills in as you add reports and history.</Empty>}
            {items.map((t) => (
              <div className="row" key={`${t.kind}-${t.id}`}>
                <div><b>{t.title}</b><p className="muted">{t.detail}</p></div>
                <div style={{ textAlign: "right" }}><Badge>{t.kind.replace(/_/g, " ")}</Badge><p className="muted">{fmtDate(t.occurred_at)}</p></div>
              </div>
            ))}
          </Card>
        )}
      </Load>
    </Page>
  );
}
