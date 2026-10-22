# 自托管 Redis（生产）——替代按命令计费的 Upstash，成本固定（VM 月费封顶）。
# 密码经 Fly secret 注入（REDIS_PASSWORD），不进镜像、不进仓库。
# 持久化：AOF everysec，落在挂载卷 /data（见 fly.toml [[mounts]]）——
# 预算已耗与计费事件不能丢；丢了的兜底是 campaigns.spent_today 定时回写。
FROM redis:7-alpine

# 必须用 shell 形式启动：exec 形式的 CMD 不做变量展开，$REDIS_PASSWORD 注不进去。
#
# REDIS_MAXMEMORY 可通过 Fly secret/env 调整（默认 350mb ≈ 512MB 机器的 70%，
# 留出进程与 AOF 缓冲，避免 OOM）。淘汰策略选 volatile-lru 而非 allkeys-lru：
# 只淘汰带 TTL 的键（freq / bid 本来就该过期）；预算键与 Stream 队列没有 TTL，
# 绝不会被淘汰——队列里是计费事件，被逐出就是资损。
CMD sh -c 'exec redis-server \
    --appendonly yes \
    --appendfsync everysec \
    --dir /data \
    --requirepass "$REDIS_PASSWORD" \
    --maxmemory "${REDIS_MAXMEMORY:-350mb}" \
    --maxmemory-policy volatile-lru'
