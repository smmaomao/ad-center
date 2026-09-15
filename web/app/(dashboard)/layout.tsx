import { redirect } from "next/navigation";
import { getSession, getNavTree, roleLabel } from "@/lib/auth";
import { Sidebar } from "@/components/sidebar";
import { logoutAction } from "./logout-action";

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");

  return (
    <div className="flex min-h-screen">
      <Sidebar
        items={await getNavTree()}
        email={session.email}
        roleLabel={roleLabel(session.role)}
        logout={logoutAction}
      />
      <main className="app-canvas flex-1 overflow-y-auto">
        <div className="relative z-10 mx-auto w-full max-w-7xl px-6 py-8">
          {children}
        </div>
      </main>
    </div>
  );
}
