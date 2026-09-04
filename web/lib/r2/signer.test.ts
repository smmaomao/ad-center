// SigV4 签名器对拍测试 —— 与 Go internal/storage/signer_test.go 使用相同
// AWS 官方测试向量（S3 开发者指南 "Authenticating Requests: Using Query
// Parameters"），双实现共享权威向量即互为正确性证明。
// 运行：node --test lib/r2/signer.test.ts（Node ≥ 23.6 原生 TS）
import { test } from "node:test";
import assert from "node:assert/strict";
import { presignURL, presignPUT, type R2Config } from "./signer.ts";

test("AWS 官方 SigV4 查询串向量（us-east-1 / examplebucket / test.txt）", () => {
  const got = presignURL(
    "examplebucket.s3.amazonaws.com",
    "AKIAIOSFODNN7EXAMPLE",
    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "us-east-1",
    "GET",
    "test.txt",
    86400,
    "20130524T000000Z",
  );
  const want =
    "https://examplebucket.s3.amazonaws.com/test.txt" +
    "?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
    "&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
    "&X-Amz-Date=20130524T000000Z" +
    "&X-Amz-Expires=86400" +
    "&X-Amz-SignedHeaders=host" +
    "&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404";
  assert.equal(got, want);
});

test("对象 key 含空格/中文时逐段编码（斜杠保留）", () => {
  const url = presignURL(
    "acct.r2.cloudflarestorage.com",
    "ak",
    "sk",
    "auto",
    "PUT",
    "bucket/creatives/ab/cr 素材.mp4",
    600,
    "20260904T000000Z",
  );
  assert.ok(
    url.includes("/bucket/creatives/ab/cr%20%E7%B4%A0%E6%9D%90.mp4?"),
    `object key not segment-encoded: ${url}`,
  );
  assert.ok(url.includes("X-Amz-Credential=ak%2F"), `credential slash not encoded: ${url}`);
});

test("PUT 与 GET 签名不同（method 必须进规范请求）", () => {
  const args = [
    "examplebucket.s3.amazonaws.com",
    "AKIAIOSFODNN7EXAMPLE",
    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "us-east-1",
  ] as const;
  const get = presignURL(...args, "GET", "test.txt", 3600, "20130524T000000Z");
  const put = presignURL(...args, "PUT", "test.txt", 3600, "20130524T000000Z");
  assert.notEqual(get, put);
});

test("presignPUT 生成 R2 path-style 地址（含 bucket 段）", () => {
  const cfg: R2Config = {
    accountId: "acct123",
    bucket: "ads-creatives",
    accessKeyId: "ak",
    secretAccessKey: "sk",
  };
  const url = presignPUT(cfg, "creatives/ab/adv-1/cr-1.mp4", 600);
  assert.ok(
    url.startsWith("https://acct123.r2.cloudflarestorage.com/ads-creatives/creatives/ab/adv-1/cr-1.mp4?"),
    `unexpected url: ${url}`,
  );
  assert.ok(url.includes("X-Amz-Expires=600"));
  assert.ok(url.includes("X-Amz-Signature="));
});
