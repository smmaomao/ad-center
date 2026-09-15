"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import type { NavNode } from "@/lib/auth";
import { Button } from "@/components/ui/button";

interface SidebarProps {
  items: NavNode[];
  email: string;
  roleLabel: string;
  logout: () => Promise<void>;
}

/** href 非空且当前路径命中（自身或后代） */
function isActive(pathname: string, href: string | null): boolean {
  if (!href) return false;
  return pathname === href || pathname.startsWith(href + "/");
}

/** 节点或任一后代被高亮——用于决定分组是否默认展开 */
function nodeContainsActive(node: NavNode, pathname: string): boolean {
  if (isActive(pathname, node.href)) return true;
  return node.children.some((c) => nodeContainsActive(c, pathname));
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="transition-transform duration-200"
      style={{ transform: open ? "rotate(90deg)" : "none" }}
    >
      <path d="m9 6 6 6-6 6" />
    </svg>
  );
}

export function Sidebar({ items, email, roleLabel, logout }: SidebarProps) {
  const pathname = usePathname();
  const [expanded, setExpanded] = useState<Set<string>>(
    () =>
      new Set(
        items.filter((n) => nodeContainsActive(n, pathname)).map((n) => n.code),
      ),
  );

  const toggle = (code: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(code)) next.delete(code);
      else next.add(code);
      return next;
    });
  };

  const initials = (email || "?").slice(0, 1).toUpperCase();

  return (
    <aside className="sidebar-surface relative z-10 flex w-56 shrink-0 flex-col text-sidebar-foreground">
      {/* Brand */}
      <div className="flex items-center gap-3 px-5 py-5">
        <div className="grid h-10 w-10 place-items-center rounded-xl bg-gradient-to-br from-brand to-amber-300/70 shadow-lg shadow-brand/20 ring-1 ring-white/15">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="oklch(0.20 0.03 72)" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="2.5" fill="oklch(0.20 0.03 72)" stroke="none" />
            <path d="M7.5 7.5a6.5 6.5 0 0 0 0 9M16.5 7.5a6.5 6.5 0 0 1 0 9" />
            <path d="M4.5 4.5a11 11 0 0 0 0 15M19.5 4.5a11 11 0 0 1 0 15" opacity="0.5" />
          </svg>
        </div>
        <div className="leading-tight">
          <p className="text-[15px] font-bold tracking-tight text-white">广告中心</p>
          <p className="text-[11px] font-medium text-white/45">自有广告投放管理</p>
        </div>
      </div>

      <div className="mx-4 h-px bg-white/10" />

      {/* Nav */}
      <nav className="flex-1 space-y-4 overflow-y-auto px-3 py-4">
        {items.map((node) => (
          <NavItemView
            key={node.code}
            node={node}
            pathname={pathname}
            expanded={expanded}
            toggle={toggle}
            depth={0}
          />
        ))}
      </nav>

      {/* User */}
      <div className="mx-3 mb-3 rounded-xl bg-white/5 p-3 ring-1 ring-white/10">
        <div className="flex items-center gap-3">
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-brand/20 text-sm font-semibold text-brand ring-1 ring-brand/30">
            {initials}
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium text-white">{email}</div>
            <div className="text-[11px] text-white/45">{roleLabel}</div>
          </div>
        </div>
        <form action={logout} className="mt-3">
          <Button
            type="submit"
            variant="outline"
            size="sm"
            className="w-full border-white/15 bg-white/5 text-white/80 hover:bg-white/10 hover:text-white"
          >
            退出登录
          </Button>
        </form>
      </div>
    </aside>
  );
}

function NavItemView({
  node,
  pathname,
  expanded,
  toggle,
  depth,
}: {
  node: NavNode;
  pathname: string;
  expanded: Set<string>;
  toggle: (code: string) => void;
  depth: number;
}) {
  const hasChildren = node.children.length > 0;
  const active = nodeContainsActive(node, pathname);
  const open = expanded.has(node.code);
  const indent = 8 + depth * 12;

  if (!hasChildren) {
    return (
      <Link
        href={node.href ?? "#"}
        style={{ paddingLeft: indent }}
        className={
          "group relative flex items-center rounded-lg py-2 pr-3 text-sm transition-colors " +
          (active
            ? "bg-brand/15 font-semibold text-brand"
            : "font-medium text-white/70 hover:bg-white/5 hover:text-white")
        }
      >
        {active && (
          <span className="absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r-full bg-brand" />
        )}
        {node.label}
      </Link>
    );
  }

  return (
    <div>
      <div
        className="flex items-center rounded-lg transition-colors hover:bg-white/5"
        style={{ paddingLeft: indent - 4 }}
      >
        {node.href ? (
          <Link
            href={node.href}
            onClick={() => toggle(node.code)}
            className={
              "flex-1 cursor-pointer rounded-lg py-2 text-sm font-medium transition-colors " +
              (active ? "text-brand" : "text-white/70 hover:text-white")
            }
          >
            {node.label}
          </Link>
        ) : (
          <button
            type="button"
            onClick={() => toggle(node.code)}
            className={
              "flex-1 cursor-pointer rounded-lg py-2 text-left text-sm font-medium transition-colors " +
              (active ? "text-brand" : "text-white/70 hover:text-white")
            }
          >
            {node.label}
          </button>
        )}
        <button
          type="button"
          onClick={() => toggle(node.code)}
          aria-label={open ? "折叠" : "展开"}
          className="ml-1 rounded-md p-1.5 text-white/40 transition-colors hover:bg-white/10 hover:text-white"
        >
          <Chevron open={open} />
        </button>
      </div>
      {open && (
        <div className="mt-0.5 space-y-0.5">
          {node.children.map((c) => (
            <NavItemView
              key={c.code}
              node={c}
              pathname={pathname}
              expanded={expanded}
              toggle={toggle}
              depth={depth + 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}
