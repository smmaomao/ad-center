import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

interface PlaceholderProps {
  title: string;
  stage: string;
}

/** 页面占位组件：骨架阶段统一占位 */
export function Placeholder({ title, stage }: PlaceholderProps) {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">{title}</h1>
      <Card>
        <CardHeader>
          <CardTitle>建设中</CardTitle>
          <CardDescription>{stage}</CardDescription>
        </CardHeader>
      </Card>
    </div>
  );
}
