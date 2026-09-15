import { requireMenu } from "@/lib/auth";
import { getDecisionCache } from "@/lib/go-api";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { DecisionCacheForm } from "../decision-cache-form";

export const dynamic = "force-dynamic";

export default async function CacheSettingsPage() {
  const session = await requireMenu("/settings/cache");

  const initial = await getDecisionCache(session.email).catch(() => ({
    enabled: true,
    ttl_seconds: 300,
  }));

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">决策结果缓存</h1>
        <p className="text-sm text-muted-foreground">
          Redis 缓存决策结果，降低引擎计算与预算 / 频控读取压力
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>缓存开关与时长</CardTitle>
        </CardHeader>
        <CardContent>
          <DecisionCacheForm initial={initial} />
        </CardContent>
      </Card>
    </div>
  );
}
