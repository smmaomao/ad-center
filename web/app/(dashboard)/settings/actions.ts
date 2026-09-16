"use server";

import { revalidatePath } from "next/cache";
import { getSession } from "@/lib/auth";
import {
  updateDecisionCache,
  updatePricingBenchmark,
  type PricingBenchmark,
  GoApiError,
} from "@/lib/go-api";

export interface FormState {
  error?: string;
  ok?: boolean;
}

function str(fd: FormData, key: string): string {
  const v = fd.get(key);
  return typeof v === "string" ? v.trim() : "";
}

/** 更新决策缓存配置（表单提交） */
export async function updateDecisionCacheAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const enabled = fd.get("enabled") !== null;
  const ttl = Number.parseInt(str(fd, "ttl_seconds"), 10);
  if (!Number.isFinite(ttl) || ttl <= 0 || ttl > 3600) {
    return { error: "缓存时长需为 1~3600 秒" };
  }

  try {
    await updateDecisionCache(session.email, { enabled, ttl_seconds: ttl });
  } catch (e) {
    return { error: e instanceof GoApiError ? e.message : "保存失败" };
  }
  revalidatePath("/settings");
  return { ok: true };
}

/** 更新平台计费标准线（打分引擎的 100 分基准） */
export async function updatePricingBenchmarkAction(
  _prev: FormState,
  fd: FormData,
): Promise<FormState> {
  const session = await getSession();
  if (!session) return { error: "会话已过期，请重新登录" };

  const num = (key: string): number => {
    const v = Number.parseFloat(str(fd, key));
    return Number.isFinite(v) ? v : 0;
  };
  const b: PricingBenchmark = {
    cpm: num("cpm"),
    cpc: num("cpc"),
    cpa_install: num("cpa_install"),
    cpa_activate: num("cpa_activate"),
    cpa_register: num("cpa_register"),
    cpa_first_purchase: num("cpa_first_purchase"),
  };
  // 六项都是分母，必须为正，否则出价基准分会被放大成天文数字
  if (Object.values(b).some((v) => v <= 0)) {
    return { error: "六项标准线都必须为正数（它们是出价基准分的分母）" };
  }

  try {
    await updatePricingBenchmark(session.email, b);
  } catch (e) {
    return { error: e instanceof GoApiError ? e.message : "保存失败" };
  }
  revalidatePath("/settings");
  return { ok: true };
}
