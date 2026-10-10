import { requireMenu } from "@/lib/auth";
import { getLoopable } from "@/lib/go-api";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { LoopableForm } from "../loop-form";

export const dynamic = "force-dynamic";

export default async function LoopSettingsPage() {
  const session = await requireMenu("/settings/loop");

  const initial = await getLoopable(session.email).catch(() => ({
    enabled: false,
  }));

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">广告列表循环开关</h1>
        <p className="text-sm text-muted-foreground">
          总控开关：开启后，下发给客户端的广告列表 data.loopable 强制为 true
          （客户端可在缓存期内循环播放）；关闭则强制为 false。未配置时回退到按素材类型推导的历史行为。
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>循环播放总开关</CardTitle>
        </CardHeader>
        <CardContent>
          <LoopableForm initial={initial} />
        </CardContent>
      </Card>
    </div>
  );
}
