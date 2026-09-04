import { requireRole } from "@/lib/auth";
import { AdvertiserForm } from "../advertiser-form";

export const dynamic = "force-dynamic";

export default async function NewAdvertiserPage() {
  await requireRole(["super_admin", "operator"]);
  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">新增广告主</h1>
        <p className="text-sm text-muted-foreground">
          创建后可在详情页上传素材并配置投放
        </p>
      </div>
      <AdvertiserForm />
    </div>
  );
}
