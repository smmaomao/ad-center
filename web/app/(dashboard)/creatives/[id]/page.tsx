import { requireMenu } from "@/lib/auth";
import { listCreatives, listAdvertisers, listApps } from "@/lib/go-api";
import { CreativeForm } from "../creative-form";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export const dynamic = "force-dynamic";

export default async function EditCreativePage({
  params,
}: PageProps<"/creatives/[id]">) {
  const session = await requireMenu("/creatives");
  const { id } = await params;

  const [creatives, advertisers, apps] = await Promise.all([
    listCreatives(session.email).catch(() => []),
    listAdvertisers(session.email).catch(() => []),
    listApps(session.email).catch(() => []),
  ]);
  const initial = creatives.find((c) => c.id === id);

  if (!initial) {
    return (
      <Card className="border-destructive">
        <CardHeader>
          <CardTitle className="text-destructive">素材不存在</CardTitle>
        </CardHeader>
        <CardContent>可能已被删除</CardContent>
      </Card>
    );
  }

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{initial.name}</h1>
        <p className="text-sm text-muted-foreground">编辑素材配置</p>
      </div>
      <CreativeForm advertisers={advertisers} apps={apps} initial={initial} />
    </div>
  );
}
