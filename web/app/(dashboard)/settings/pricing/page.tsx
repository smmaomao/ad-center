import { requireMenu } from "@/lib/auth";
import {
  getPricingBenchmark,
  DEFAULT_PRICING_BENCHMARK,
} from "@/lib/go-api";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { PricingBenchmarkForm } from "../pricing-benchmark-form";

export const dynamic = "force-dynamic";

export default async function PricingSettingsPage() {
  const session = await requireMenu("/settings/pricing");

  const benchmark = await getPricingBenchmark(session.email).catch(
    () => DEFAULT_PRICING_BENCHMARK,
  );

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold">平台计费标准线</h1>
        <p className="text-sm text-muted-foreground">
          打分引擎的 100 分基准：素材出价基准分 = 实际出价 ÷ 对应标准线 × 100
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>计费标准线配置</CardTitle>
        </CardHeader>
        <CardContent>
          <PricingBenchmarkForm initial={benchmark} />
        </CardContent>
      </Card>
    </div>
  );
}
