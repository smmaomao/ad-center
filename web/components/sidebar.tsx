"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { NavItem } from "@/lib/auth";
import { Button } from "@/components/ui/button";

interface SidebarProps {
  items: NavItem[];
  email: string;
  roleLabel: string;
  logout: () => Promise<void>;
}

export function Sidebar({ items, email, roleLabel, logout }: SidebarProps) {
  const pathname = usePathname();

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r bg-background">
      <div className="border-b px-4 py-5">
        <p className="text-lg font-semibold">广告中心</p>
        <p className="text-xs text-muted-foreground">自有广告投放管理</p>
      </div>

      <nav className="flex-1 space-y-1 p-2">
        {items.map((item) => {
          const active =
            pathname === item.href || pathname.startsWith(item.href + "/");
          return (
            <Link
              key={item.href}
              href={item.href}
              className={
                "block rounded-md px-3 py-2 text-sm " +
                (active
                  ? "bg-primary/10 font-medium text-primary"
                  : "text-muted-foreground hover:bg-muted hover:text-foreground")
              }
            >
              {item.label}
            </Link>
          );
        })}
      </nav>

      <div className="space-y-2 border-t p-4">
        <div className="truncate text-sm font-medium" title={email}>
          {email}
        </div>
        <div className="text-xs text-muted-foreground">{roleLabel}</div>
        <form action={logout}>
          <Button variant="outline" size="sm" className="w-full">
            退出登录
          </Button>
        </form>
      </div>
    </aside>
  );
}
