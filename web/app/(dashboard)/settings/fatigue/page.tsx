import { requireMenu } from "@/lib/auth";
import { getFatigueConfig, DEFAULT_FATIGUE_CONFIG } from "@/lib/go-api";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { FatigueConfigForm } from "../fatigue-config-form";

export const dynamic = "force-dynamic";

export default async function FatigueSettingsPage() {
  const session = await requireMenu("/settings/fatigue");

  const fatigue = await getFatigueConfig(session.email).catch(
    () => DEFAULT_FATIGUE_CONFIG,
  );

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">全局频控配置</h1>
        <p className="text-sm text-muted-foreground">
          按「用户 × 素材」双重限流：短窗口内观看上限 + 每日观看上限，超出后
          该素材在窗口结束前对该用户自动隐藏
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>疲劳度控制开关</CardTitle>
          <CardDescription>
            开启后保存即生效，无需重启服务
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FatigueConfigForm initial={fatigue} />
        </CardContent>
      </Card>
    </div>
  );
}
