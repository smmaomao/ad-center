import { requireMenu } from "@/lib/auth";
import { listApps } from "@/lib/go-api";
import { CreativeForm } from "../creative-form";

export const dynamic = "force-dynamic";

export default async function NewCreativePage() {
  const session = await requireMenu("/creatives");
  const apps = await listApps(session.email).catch(() => []);

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">新建素材</h1>
        <p className="text-sm text-muted-foreground">
          上传广告内容，并定义它可以投放到哪些 App、以什么样式展现
        </p>
      </div>
      <CreativeForm apps={apps} />
    </div>
  );
}
