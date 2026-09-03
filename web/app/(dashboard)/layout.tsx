import { redirect } from "next/navigation";
import { createClient } from "@/lib/supabase/server";
import {
  ROLE_LABELS,
  getSession,
  navItemsForRole,
} from "@/lib/auth";
import { Sidebar } from "@/components/sidebar";

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await getSession();
  if (!session) redirect("/login?error=no_access");

  async function logout() {
    "use server";
    const supabase = await createClient();
    await supabase.auth.signOut();
    redirect("/login");
  }

  return (
    <div className="flex min-h-screen">
      <Sidebar
        items={navItemsForRole(session.role)}
        email={session.email}
        roleLabel={ROLE_LABELS[session.role]}
        logout={logout}
      />
      <main className="flex-1 overflow-y-auto bg-muted/40 p-6">{children}</main>
    </div>
  );
}
