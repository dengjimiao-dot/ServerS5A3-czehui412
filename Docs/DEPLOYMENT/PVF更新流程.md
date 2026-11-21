# PVF 更新流程（含客户端资源）

> **本册管 PVF / EXE / npk / DLL 等客户端资源发布。** 源码与登录器更新 →《源码更新手册.md》；从零搭建 →《部署手册.md》。环境常量（三副本路径、端口、compose、当前版本）见《部署手册》§0。
> 当前线上：**PVF `f16a72d1`**（129,680,425B；活跃度奖励代币 500/500/5500→200/500/1000，其余保留 15 个深渊任务 `[seeking] 3037 1`、201801 `RepeatFrom=25/0/0` 24 套循环、商城三区块）｜**EXE `2551b8a7`**（112,828,416B；活跃度面板文字补丁 + 多买多送 FE_TAIL 文案改写指针）｜content `1.2.27`(sourceSequence 32)｜bootstrap seq 148｜bundle `1.0.24`。服务端 `servers5a3-local:dev`（patch `3e4a0fe0` 循环结算）已上线。
> 玩家侧机制：登录器在线拉 bootstrap → 与 content manifest 逐文件比对 → 只下载差异 blob（Range 断点续传）→ 事务替换；任意历史版本一次启动追平，**资源更新不需要重发登录器**。

## 快速开始（90% 场景三步）

```powershell
# 1. 新 PVF 放到 F:\90cn\轻装上阵\client\Script.pvf
# 2. 服务端发布（三副本 + 8 配置 + 6 索引 + 重建容器，约 4.5 分钟）
cd F:\90cn\轻装上阵
python deploy\tools\PVF-CLI\release_pvf.py
# 3. content 通道发布（玩家自动增量下载，见 §4）
```
末尾出现 `完成，用时 … | PVF <sha前16>` 且 §6 验收全绿即完成。**登录器不重建**（内嵌哈希只是网关全挂时的离线兜底）；仅登录器代码改动才走《源码更新手册》§7。

---

## 1. 一次更新要动什么

| 类别 | 位置 | 漏了会怎样 |
|---|---|---|
| 副本① 游戏服 | `/srv/apps/ServerS5A3/runtime/docker/data/Script.pvf` | 游戏逻辑按旧 PVF 跑 |
| 副本② GM | `/srv/apps/PurpleMagic/runtime/selfhost/assets/Script.pvf`（10001:10001, 600） | GM 解析旧 PVF |
| 副本③ GM 挂载点 | `/srv/dnf-assets/Script.pvf`（**最易漏**） | GM 容器实际读这里 |
| 配置 ×8 | `service.json`、`gateway.json`×2、`indexer.json`、`prepared.json`、`private/player-profile.json`、`gm/queue.json`、`hub/state.json`(PvfSha256 **+ sequence+1**)、`compose.env`(PM_PVF_SHA256) | 校验失败 / 登录器按 equivocation 拒绝 / rebuild 后按旧哈希建索引 |
| 索引 ×6 | `runtime/selfhost/indexes/`：`pvf.bin`、`items.json`、`item-details-v3-<sha>.bin`、`quests-v1-<sha>.bin`、`professions-v1-<sha>.bin`、`expert-jobs-v1-<sha>.json` | `pvf.bin` 旧 → GM 崩溃循环 |
| content 通道 | `hub/content-blobs/<sha>` + `releases/channels/stable/manifest.signed.json` + check/publish | 玩家无法自动追平 |
| 登录器 | **不需要动**（在线追平） | — |

四个关键事实：① PVF 解密密钥是服务端内置常量，换文件即可解析；② 索引文件名带 PVF 校验和，旧校验和文件是死数据必须删；③ `pvf.bin` 不匹配 → GM **崩溃循环**（不是警告）；④ **手工改 `hub/state.json` 前必须 `docker stop purplemagic-gateway-1`**（运行中网关任何管理 API 请求都会把内存旧状态写回磁盘覆盖迁移，事故①/④）。

---

## 2. 多买多送三区块保护（每次 PVF 更新前后必做 ⚠）

服务端商城逻辑依赖 `etc/chn_cerashop_multibonus.etc` 的三个区块：`[private festival periods]`/`[private festival commodities]`/`[private festival rewards]`（大小写敏感）。丢失 → catalog fail-closed → 全商城报「物品栏空间不足」。

```powershell
$pvf = "F:\90cn\轻装上阵\deploy\tools\PVF-CLI\dist\pvf.exe"
& $pvf read --pvf "F:\90cn\轻装上阵\client\Script.pvf" --path "etc/chn_cerashop_multibonus.etc" --format tokens --out "$env:TEMP\mb_check.json"
@('private festival periods','private festival commodities','private festival rewards') |
  ForEach-Object { "$_ : $((Get-Content $env:TEMP\mb_check.json -Raw) -match [regex]::Escape('['+$_+']'))" }   # 三个都要 True
```
被上游 PVF/operator 覆盖丢了：从上一版好 PVF 提取重插（`数据迁移\_extracted\build_festival_plan.py`，token 规则：保留 token 用 `payload_u32`、新增字符串/标签用 `text`），重插后跑《源码更新手册》§2 冒烟确认服务端兼容。**别用整文件覆盖旧 PVF 的方式回退**（会连带丢其他定制）。

**当前 201801 档位（官方循环，`b038d4c2` 起，2026-11-20 恢复；更新时务必保住）**：periods 行 201801 的 `RepeatFrom/RepeatItem/RepeatCount` = `25/490701763/1`（第 25 套起每套发一个兽人族宝物盒，无限循环）；`[private festival rewards]` 里 201801 只有 **22 档**：at=2..12 + at=14..24（跳过 13），item 见备份 `Script.pvf.bak-preloop-b038d4c2`。

> **⚠ 血泪教训：不要用「12 套循环铺到 at=10000」的改法**（曾产出 `d7a6b3a2`，2026-11-20 回退）。服务端下发 packet `0x7f02` 每期固定 256 字节、**最多 24 档**（`private_festival_wire.go` `len(tiers)>24` → 静默 `status=5` 空帧，不打日志），客户端 `festival_ui.h` 也校验 `>24` 拒收。铺 9166 档的后果：游戏内「礼包多买多送」界面**空白**（只显示"本账号已购买 -- 套 / 第1/1页"，无道具行）。**协议只支持单一奖励循环（RepeatFrom），做不到 11 种奖励交替循环**——除非改服务端结算+帧结构+客户端 UI 并重编。`festival.Parse` 硬约束：`at≤10000`、`count≤9999`、`RepeatFrom>0` 时禁止 `at≥RepeatFrom` 的奖励行、档位数≤24。附录 C 的 12 套循环方案已废弃，勿再执行。
>
> **✅ 现行做法（2026-11-20 seq131 上线）：24 套整轮循环，服务端 patch + PVF 配合，无需改帧结构/插件**。PVF 201801 行写 `RepeatFrom=25 / RepeatItem=0 / RepeatCount=0` 作循环标记，档位保留 ≤24 个模板（如 at=2..12+14..24 共 22 档）。服务端 patch `3e4a0fe0` 识别该标记后，在第 N≥25 套把套数映射回 `(at-1)%(RepeatFrom-1)+1` 重发模板档位（`Award.At` 保持真实套数，邮件去重不撞车）；发包时隐藏循环标记避免客户端显示 ×0 空卡。效果：25 套重放第 1 套（无档位→跳过）、26 套重放第 2 套（二重）…48 套重放第 24 套（至尊3）。改周期只需改 PVF `RepeatFrom`（=周期+1），服务端无需再动。详见版本台账 seq131。

---

## 3. 一键脚本（标准发布）

```powershell
cd F:\90cn\轻装上阵
python deploy\tools\PVF-CLI\release_pvf.py                   # 全 8 步
python deploy\tools\PVF-CLI\release_pvf.py --from 5         # 断点续跑（索引已建好，从上传开始）
python deploy\tools\PVF-CLI\release_pvf.py --only 7         # 单步重做（7=up+IP post-fix，8=重启验收）
python deploy\tools\PVF-CLI\release_pvf.py --pvf D:\x\Script.pvf   # 指定 PVF（不覆盖本地 client）
```

| 步 | 内容 | 耗时 |
|---|---|---|
| 1-4 | 算 SHA256 + 改 `gmindex\gmconfig.json` → `--build-pvf-index`(72M) → `--build-item-details`(208M) → `--build-quest-index`(items.json+quests) | ~50s |
| 5-6 | 上传 PVF+4 索引(345M) → 三副本 + 8 配置 + sequence+1 + 索引改名/删旧/chown | 视带宽 |
| 7-8 | `manage.py up`(python3.12) + SERVER_IP post-fix + 重建 server → 重启 GM/网关 + 验收 | 3~4min |

> `gmindex` 运行时目录在**官方克隆侧**（`deploy\PurpleMagic\gmindex`，gitignored），脚本 `GMIDX` 常量依赖此路径，勿移动。`pvf.bin` 已存在时先删；`--build-item-details` 的 `reused=true` 不重建 items.json，丢失时删 `item-details-v3-<sha>.bin` 触发全量重建。
> **两个盲区（脚本不自动做，必须手工补）**：① apply 后 `content.files` 里 Script.pvf 的 **Length 不随哈希更新** → 玩家下载体积校验 404；修法：停网关 → python 改 `state.json` 的 Length 为真实字节数 → 启网关。② manifest 必须**复制入位** `releases/channels/stable/`（check 读网关自己的 ReleaseDir，scp 到 /tmp 不算数）。

---

## 4. content 通道发布（统一路径，2026-11-17 上线）

**先判断变更类型**（EXE/PVF 与 manifest 必须配对）：

| 变更 | 路径 | 玩家侧 |
|---|---|---|
| PVF 换版 | §3 一键脚本 **+** 本节 | 自动下载 130MB 追平 |
| EXE 换版 | **《源码更新手册》§8 六步**（含 profile 双改）+ 本节 | 自动下载 113MB |
| npk / DLL（不换 PVF/EXE） | **只走本节** | 自动下载，不动 profile |
| 登录器代码改动 | 《源码更新手册》§7 构建 + bundle POST | 登录器自更新 |
| 反作弊策略 | 两处 profile 的 AntiCheat 段（停网关）→ seq+1 | 下次启动生效 |

**工具与密钥（本机）**：`数据迁移\_extracted\make_content_manifest.py`（最小清单模式）、私钥 `deploy\launcher-input\manifest-key.pem`（公钥已登记网关 Source `self-manifest`）；管理 API `https://127.0.0.1:18444`（**HTTPS 仅本机**，Bearer = `runtime/selfhost/admin-token.txt`）。
**发布工具（三册共用）**：`deploy\tools\release\fix_exe_manifest.py`（EXE 换版重签）、`publish_content.py`（服务器 check/publish）、`verify_bootstrap.py`（自洽验证）。

### 4.1 生成签名 manifest
```powershell
cd F:\90cn\轻装上阵
python 数据迁移\_extracted\make_content_manifest.py `
  --key deploy\launcher-input\manifest-key.pem --version <1.x.0> --sequence <递增> `
  --out 数据迁移\_extracted\manifest.signed.json `
  --file "client/DNF.exe|F:\90cn\轻装上阵\client\DNF.exe|downloadable" `
  --file "client/Script.pvf|F:\90cn\轻装上阵\client\Script.pvf|downloadable" `
  --file "client/ImagePacks2/sprite_character.NPK|F:\90cn\轻装上阵\client\ImagePacks2\sprite_character.NPK|downloadable" `
  --file "client/CombatPower.dll|F:\90cn\轻装上阵\client\CombatPower.dll|downloadable" `
  --file "client/DpsMeter.dll|F:\90cn\轻装上阵\client\DpsMeter.dll|downloadable"
```
> **Version / Sequence 都必须递增**（重复按 equivocation 拒绝）；清单必须含配对 `client/DNF.exe` + `client/Script.pvf`，且哈希与批准 profile 一致（否则 `check` 的 compatible=false）；`Length` 由工具自动取本地文件真实字节数。EXE 换版可直接跑 `fix_exe_manifest.py`（复用现有清单、只改 DNF.exe + 自动递增）。

### 4.2 blob 与 manifest 入位
```powershell
# 每个 downloadable 文件：原文件直传，文件名 = 其 sha256（64 位小写）；PVF 也可服务器本地 cp
scp <文件路径> root@124.221.247.221:/srv/apps/PurpleMagic/runtime/selfhost/hub/content-blobs/<sha256>
scp 数据迁移\_extracted\manifest.signed.json root@124.221.247.221:/srv/apps/PurpleMagic/runtime/selfhost/releases/channels/stable/manifest.signed.json
scp deploy\tools\release\publish_content.py root@124.221.247.221:/tmp/
```

### 4.3 check → publish（服务器本机）
```bash
python3 /tmp/publish_content.py                 # 默认 self-manifest / stable；check compatible=true 才 publish
```
> 也可用 curl（Bearer 直连认证免 login/CSRF）：`POST /admin/api/check {"id":"self-manifest","channel":"stable"}` 拿 `candidateId` → `POST /admin/api/publish {"candidateId":"<id>","confirmCompatibility":true}`。token 传递易被 shell 处理错，推荐 python 脚本。

### 4.4 硬约束（每条都出过事故）
1. **`content.files["DNF.exe"].sha256` 必须 == `profile.ExeSha256`**、Script.pvf 同理（客户端 `Contracts.cs` 校验；不一致 → 整份 bootstrap 拒收 → 旧登录器不更新、全服卡「请先完成客户端资源更新」）。
2. **Length 必须等于真实字节数**（一键脚本 apply 只换哈希不换 Length → blob 404）。
3. manifest 必须入位 `releases/channels/stable/`；blob 必须入位 `hub/content-blobs/`（文件名=sha）。
4. 白名单外路径被**静默过滤**（check 仍 compatible）——发布后核对 bootstrap 里文件数；`.dll` 已在网关白名单放行。
5. PVF/EXE 换版：**先**完成 profile 迁移（停网关改 state，见《源码更新手册》§8）**再** check/publish；运行中网关的 publish/save 会把内存旧 profile 写回磁盘。
6. 发布后固定 `verify_bootstrap.py`：`MATCH(profile==content): True`、`blob Range: 206`、sequence 已 +1。

---

## 5. 手工兜底（仅当一键脚本不可用）

> 编号与脚本步骤对应；脚本可用时**不要**走这里（历史上漏过 `/srv/dnf-assets` 与索引）。

```powershell
# ① 本机：算哈希，改 deploy\PurpleMagic\gmindex\gmconfig.json 的 pvfSha256 / pvfPath
(Get-FileHash "F:\90cn\轻装上阵\client\Script.pvf" -Algorithm SHA256).Hash.ToLower()   # <NEW_SHA>
# ② 本机：按顺序建索引（后两条依赖 pvf.bin；gmtool 一次只能跑一个操作）
cd F:\90cn\轻装上阵\deploy\PurpleMagic\gmindex
Remove-Item pvf.bin -ErrorAction SilentlyContinue
.\gmtool.exe --config .\gmconfig.json --build-pvf-index
.\gmtool.exe --config .\gmconfig.json --build-item-details     # 必须 failed=0
.\gmtool.exe --config .\gmconfig.json --build-quest-index
# ③ 上传 PVF + 4 索引（*.new 后缀）到 /tmp 与 indexes/
```
```bash
# ④ 服务器：三副本 + 8 配置 + sequence+1 + 索引改名/删旧（写脚本 scp 执行，勿多层引号内联）
#    要点：OLD=state.json 的 published.profile.PvfSha256；三副本先 .bak-<OLD前12> 再换，属主/权限见部署手册 §6.4
#    8 处配置用文本替换 OLD→NEW；state.json 的 published.sequence +1；indexes 删含 OLD 前16 的旧文件
# ⑤ up（120 频道基线）+ post-fix：完全按《源码更新手册》§4 ④⑤
# ⑥ 重启 GM/网关 + 验收
docker restart purplemagic-gm-1 purplemagic-gateway-1
docker logs purplemagic-gm-1 --since 2m 2>&1 | grep -iE 'ready|mismatch|fatal'
sha256sum /srv/dnf-assets/Script.pvf /srv/apps/ServerS5A3/runtime/docker/data/Script.pvf /srv/apps/PurpleMagic/runtime/selfhost/assets/Script.pvf
```

---

## 6. 验收清单

- [ ] 三副本 `sha256sum` 全 = `<NEW_SHA>`；`indexes/` 无旧 SHA 残留、属主 10001
- [ ] bootstrap（`verify_bootstrap.py` 或服务器 `curl -sk https://127.0.0.1:8443/launcher/v2/bootstrap.json` 解码**大写 `Payload`**）：sequence +1、PvfSha256/ExeSha256 新值、content 配对一致、launcher 版本不变
- [ ] content：manifest 入位、`content-blobs/<sha>` 存在、Script.pvf 的 `length` = 真实字节数、blob Range 206
- [ ] `purplemagic-gm-1` Up（非 Restarting），日志 `external GM service ready` 无 `mismatch`
- [ ] 游戏服 healthy、0 `level=ERROR|panic`、`dnfbridge started` 时间新于发布、`game_ports=120`
- [ ] GM 面板能搜到新 PVF 新增道具，名称正确
- [ ] 旧客户端实测：登录器启动 → content 下载 → 进游戏（登录器本体不用动）
- [ ] 游戏内验证本次内容；多买多送三区块仍在（§2）

---

## 7. 常见坑

| 坑 | 解法 |
|---|---|
| GM 崩溃循环 `PVF disk index header/source mismatch` | `pvf.bin` 旧；本机 `--build-pvf-index` 重传，先 `docker stop purplemagic-gm-1` 止血 |
| GM 崩溃循环 `item index cache does not match the loaded PVF` | `items.json` 的 `pvfChecksum` 仍是旧 PVF：本机已有 `item-details-v3-<新sha>.bin` → `--build-item-details` 走 `reused=true` 跳过重建（**回退到发布过的旧 PVF 时必踩**）。修：删本地 `item-details-v3-<新sha>.bin` → 重跑 `--build-item-details`（`reused=false`，checksum 更新）→ 重传 `items.json`+该 bin 入位 → `up -d --force-recreate gm`。发布后务必 `head -c 200 indexes/items.json` 核对 `pvfChecksum` |
| 服务器 python 版本 | manage.py / prepare.py 一律 `python3.12`（默认 3.9 不支持 `write_text(newline=)`） |
| up 后 SERVER_IP 变内网 / db 3307 丢 | 《源码更新手册》§4 ⑤ post-fix；`--force-recreate` |
| compose 项目名错 / restart 不重读 env | `-p servers5a3-local` 必带；env 改动必须 `--force-recreate` |
| 「假成功」：文件全绿但进程持旧 PVF | 比对容器 `StartedAt` 与日志 `dnfbridge started` 时间（§6） |
| `/srv/dnf-assets` 最易漏 | GM 容器挂这里，不是 `selfhost/assets` |
| sequence 没 +1 / 倒退 | 客户端按 equivocation 拒绝；只增不减 |
| 属主错 | PurpleMagic 侧 10001:10001（assets 600、pvf.bin/items.json 644、其余 600） |
| SSH 多层引号炸 | 复杂 python 写脚本 scp 上去执行，或 base64 管道 |
| 索引积压 | 两侧按 SHA 积历史 `item-details/quests`（各 218M）；定期只留当前 SHA + pvf.bin/items.json |
| 客户端弹 InIT（安装文件发生错误） | PVF 与定制 DNF.exe 不配套；换 PVF 来源前**先本地双击 DNF.exe 验证能进游戏**再发布（服务端全绿不代表客户端兼容） |
| Windows subprocess GBK 崩 | Python 调 gmtool/ssh/pvf.exe 用 bytes + `decode(errors='replace')`，别用 `text=True` |
| 删商城条目后全商城「物品栏空间不足」 | multibonus/coupon 引用失效 → catalog fail-closed；补丁已降级跳过，定位 `TestRealPVFCeraShopCatalogResolvesAtLeastOneSupportedProduct` |
| 再次 up 被 check_network 拒 | post-fix 把 server_ip 改公网了；up 前先 `sed -i s/124.221.247.221/10.253.90.2/ instance.json` |
| 全服卡「请先完成客户端资源更新」 | profile 与 content 的 EXE 哈希不一致（§4.4 第 1 条 / 《源码更新手册》§8） |
| 回滚 | 所有被换文件有 `.bak-<旧sha12>`；回滚 = 换回文件 + 哈希改回 + 旧索引恢复 + sequence 再 +1 + 重启 |

---

## 附录 A：PVF 内容补丁（改档后发布）

工具 `deploy\tools\PVF-CLI\dist\pvf.exe`（`info|list|read|search|export|patch|verify`）。流程：**审计 → plan JSON → patch → 同一审计复核 → 冒烟 → §3 发布**；patch 产物 `output_sha256` 即 `<NEW_SHA>`。

```powershell
cd deploy\_local\ServerS5A3\go-server
go run ./cmd/scratch/giftboxaudit -pvf <pvf> -out ..\tools\PVF-CLI\audit.json
cd ..\tools\PVF-CLI
python make_giftbox_plan.py && dist\pvf.exe patch --pvf <pvf> --plan plan_giftbox.json --out candidate.pvf
$env:DNFBRIDGE_REAL_PVF_SMOKE="<candidate.pvf>"; cd ..\..\ServerS5A3\go-server
go test -run TestRealPVFSeriaShopPricingSmoke -v -count=1 ./internal/services/dnfbridge
```
要点：整批修（礼盒 `[expiration date]` 必须 `2028-12-31 06:00:00`）；锚点只能是「标签后第一个值 token 之后」；旧 token 保留 `type+payload_u32`（0 不能被 omitempty 掉）、新增字符串只给 `type+text`；定位用 `pvf read --format tokens` 的 token 顺序；标签大小写先用 `pvf search` 在原版 PVF 确认；服务端拒绝原因看容器内 `logs/packet_log.txt`。

## 附录 B：版本台账（摘要）

> `sequence` 是 hub 发布序号（每次发布 +1），与「第 N 次更新」无关。完整历史（每版事故与细节）在 git `patches` 提交、服务器 `.bak-*` 备份与 `数据迁移\_extracted` 产物清单中。

| 日期 | PVF | EXE | 说明 |
|---|---|---|---|
| 2026-11-17 (70-71) | `b038d4c2`(129,981,010) | `28e04d1d` | content 通道首发全流程；多买多送三区块保留；登录器 1.0.12 |
| 2026-11-19 白天 | `b038d4c2`（未变） | `58e51fba`(112,816,128) | **源码发布 + EXE 换版**：bundle 1.0.23、120 频道基线、Dockerfile 镜像源入补丁栈 |
| 2026-11-19 晚 (seq 94) | `b038d4c2`（未变） | `58e51fba` | **补齐 content 通道**（manifest 1.2.0 / seq 5、新 EXE blob），修复 bootstrap 自洽、恢复登录器自更新 |
| 2026-11-19 深夜 (seq 100) | `d7a6b3a2`(130,004,636) | `58e51fba` | **多买多送 201801 改 12 套循环**：`RepeatFrom=25/Repeat=490701763` 归零 + 追加 26~10000 档（跳过 25/37/49…，26 起重发二重→至尊礼盒3）。pvf.exe patch/verify、manifest 1.2.4/seq9、三副本+content 全绿、登录器 1.0.24 不变 |
| 2026-11-19 深夜 (seq 102) | `d7a6b3a2`（未变） | `2d5357f1`(112,828,416) | **EXE 去水印植入版换版**：`client/DNF.exe` 内嵌抹水印代码（`_embed_wipe.py`），`release_exe.py` 一键发布；manifest 1.2.5/seq10、两处 profile ExeSha+SecondClient 双改、sequence 100→101(content publish→102)，MATCH=True/blob 206/登录器 1.0.24 不变。回滚备份 `*.bak-pre-exe2d53` |
| 2026-11-20 凌晨 (seq 104) | `d7a6b3a2`（未变） | `7dee69a8`(112,828,416) | **EXE 去水印植入版 v2**：本地 `DNF.exe.wipe-candidate-v2` 覆盖 `client/DNF.exe` 后 `release_exe.py` 发布；manifest 1.2.6/seq11、两处 profile ExeSha+SecondClient 双改（白名单增至 3）、sequence 102→103(content publish→104)，MATCH=True/blob 206/登录器 1.0.24 不变。回滚备份 `*.bak-pre-exe7dee69` |
| 2026-11-20 凌晨 (seq 106) | `d7a6b3a2`（未变） | `23eddd7f`(112,828,416) | **EXE 去水印植入版 v3**：本地 `DNF.exe` 再更新后 `release_exe.py` 发布；manifest 1.2.7/seq12、两处 profile ExeSha+SecondClient 双改（白名单增至 4）、sequence 104→105(content publish→106)，MATCH=True/blob 206/登录器 1.0.24 不变。回滚备份 `*.bak-pre-exe23eddd` |
| 2026-11-20 凌晨 (seq 108) | `d7a6b3a2`（未变） | `7833e14b`(112,828,416) | **EXE 去水印植入版 v4**：本地 `DNF.exe` 再更新后 `release_exe.py` 发布；manifest 1.2.8/seq13、两处 profile ExeSha+SecondClient 双改（白名单增至 5）、sequence 106→107(content publish→108)，MATCH=True/blob 206/登录器 1.0.24 不变。回滚备份 `*.bak-pre-exe7833e1` |
| 2026-11-20 凌晨 (seq 110) | `b038d4c2`(129,981,010) | `7833e14b`（未变） | **回退 12 套循环→官方循环**：`d7a6b3a2` 的 9166 档超 packet 24 档上限，界面空白；`pvf.exe patch` 精确回退 201801 为官方 `RepeatFrom=25/490701763/1`+22 档（候选哈希恰等于旧官方版 `b038d4c2`）。`release_pvf.py` 全8步（三副本+索引+配置+重建容器）+ make_content_manifest 1.2.9/seq14 + blob + profile PvfSha 双改，sequence 108→109(content publish→110)，PVF/EXE MATCH=True/blob 206/游戏服 0 ERROR。回滚备份 `*.bak-pre-pvf-revert` |
| 2026-11-20 上午 (seq 112) | `9bc125f0`(129,981,028) | `7833e14b`（未变） | **用户自调 PVF**：本地 201801 `RepeatFrom` 调为 61（`490701763/1`+22档不变），按手册发布。`release_pvf.py` 全8步 + make_content_manifest 1.2.10/seq15 + blob + profile PvfSha 双改，sequence 110→111(content publish→112)，PVF/EXE MATCH=True/blob 206/游戏服 0 ERROR/GM ready。回滚备份 `*.bak-pre-pvf-9bc125f0` |
| 2026-11-20 上午 (seq 114) | `7f215690`(129,980,910) | `7833e14b`（未变） | **用户自调 PVF**：本地 201801 `RepeatFrom` 调为 27（`490701763/1`+22档不变），按手册发布。`release_pvf.py` 全8步 + make_content_manifest 1.2.11/seq16 + blob + profile PvfSha 双改，sequence 112→113(content publish→114)，PVF/EXE MATCH=True/blob 206/游戏服 0 ERROR/GM ready。回滚备份 `*.bak-pre-pvf-7f215690` |
| 2026-11-20 上午 (seq 116) | `b182791e`(129,980,988) | `7833e14b`（未变） | **用户自调 PVF**：本地 201801 `RepeatFrom` 调为 49（`490701763/1`+22档不变），按手册发布。`release_pvf.py` 全8步 + make_content_manifest 1.2.12/seq17 + blob + profile PvfSha 双改，sequence 114→115(content publish→116)，PVF/EXE MATCH=True/blob 206/游戏服 0 ERROR/GM ready。回滚备份 `*.bak-pre-pvf-b182791e` |
| 2026-11-20 上午 (seq 119) | `07567050`(129,980,947) | `904c10ac`(112,816,128) | **PVF+EXE 双换版**：用户自调 201801 `RepeatFrom=37` + 换新 EXE。`release_pvf.py` 全8步（改 PvfSha seq→117）→ 停网关 `update_profile_sha.py 904c10ac`（ExeSha+白名单增至6、seq→118）→ 起网关 → make_content_manifest 1.2.13/seq18（PVF+EXE 双条目）+ 双 blob → check/publish（seq→119）。PVF/EXE MATCH=True/双 blob 206/游戏服 0 ERROR/GM ready。回滚备份 `*.bak-pre-both-0756-904c` |
| 2026-11-20 上午 (seq 121) | `07567050`（未变） | `7833e14b`(112,828,416) | **EXE 换版（回到此前 v4 哈希）**：本地 `client/DNF.exe` 再更新后 `release_exe.py` 一键发布；manifest 1.2.14/seq19（只改 DNF.exe 条目）+ 新 EXE blob，两处 profile ExeSha+SecondClient 双改（白名单保持 6，已含 7833e14b）、sequence 119→120(content publish→121)，MATCH=True/blob 206/登录器 1.0.24 不变/AntiCheat audit 保持。回滚备份 `*.bak-pre-exe7833e1-20261120104253` |
| 2026-11-20 上午 (seq 123) | `7f215690`(129,980,910) | `7833e14b`（未变） | **用户自调 PVF（回到此前 7f215690，RepeatFrom=27）**：`release_pvf.py` 全8步（改 PvfSha seq→122）→ **踩坑**：本地已有 `item-details-v3-7f215690.bin` 使 `--build-item-details` 走 `reused=true`，`items.json` 的 `pvfChecksum` 仍为 07567050 → GM 崩溃循环 `item index cache does not match the loaded PVF`；删缓存全量重建+重传 items.json/bin+force-recreate GM 恢复（见 §7）→ 重签 manifest 1.2.15/seq20（只改 PVF 条目）+ PVF blob → check/publish（seq→123）。PVF/EXE MATCH=True/双 blob 206/游戏服 0 ERROR/game_ports=120/GM ready。回滚备份 `*.bak-pre-pvf-7f215690-20261120104659` |
| 2026-11-20 上午 (seq 125) | `7204efa0`(129,980,914) | `7833e14b`（未变） | **用户自调 PVF**：本地无 `7204efa0` 缓存 → `reused=false` 全量重建，未踩复用坑。`release_pvf.py` 全8步（改 PvfSha seq→124）→ 重签 manifest 1.2.16/seq21（只改 PVF 条目）+ PVF blob → check/publish（seq→125）。PVF/EXE MATCH=True/双 blob 206/游戏服 0 ERROR/game_ports=120/GM ready。回滚备份 `*.bak-pre-pvf-7204efa0-20261120110150` |
| 2026-11-20 上午 (seq 127) | `07567050`(129,980,947) | `7833e14b`（未变） | **用户自调 PVF（回到官方档 RepeatFrom=37）**：本地已有 `07567050` 缓存 → **发布前先删 `item-details-v3-07567050.bin`** 避免复用坑，`reused=false` 全量重建。`release_pvf.py` 全8步（改 PvfSha seq→126）→ 重签 manifest 1.2.17/seq22（只改 PVF 条目）+ PVF blob → check/publish（seq→127）。PVF/EXE MATCH=True/双 blob 206/三副本一致/items.json pvfChecksum=07567050/GM ready/游戏服 0 ERROR/game_ports=120。回滚备份 `*.bak-pre-pvf-07567050-20261120111154` |
| 2026-11-20 中午 (seq 129) | `8e4a05da`(129,980,923) | `7833e14b`（未变） | **用户自调 PVF（201801 加 at=28，25 档超上限，用户明确要求照发）**：`RepeatFrom=29`，档位 2..12+14..24+26..28=**25 档** > 24 → 预计游戏内界面仍空白（服务端发包 status=5 空帧，见 §2）。`release_pvf.py` 全8步（改 PvfSha seq→128、`reused=false`）→ 重签 manifest 1.2.18/seq23 + PVF blob → check/publish（seq→129）。PVF/EXE MATCH=True/双 blob 206/三副本一致/GM ready/游戏服 0 ERROR/game_ports=120。回滚备份 `*.bak-pre-pvf-8e4a05da-20261120114025` |
| 2026-11-20 中午 (seq 131) | `cf9a7e78`(129,739,594) | `7833e14b`（未变） | **24 套循环模式上线（服务端 patch + PVF 双发布）**：PVF 201801 改 `RepeatFrom=25 / RepeatItem=0 / RepeatCount=0`（循环标记），删 at=26/27/28 留 22 档模板（at=2..12+14..24，≤24 上限）。服务端 patch `3e4a0fe0`：`festival.Period.Cycle()` + `Apply` 把第 N≥25 套映射回 `(at-1)%24+1` 重发档位（Award.At 仍真实套数→邮件去重不撞），wire 发包隐藏循环标记避免 ×0 空卡；4 个单测覆盖循环/去重/旧模式/Parse。先发服务端（镜像 tag `pre-cycle-20261120`，healthy/0 ERROR/game_ports=120）再发 PVF（`release_pvf.py` seq→130 `reused=false` + 重签 manifest 1.2.19/seq24 + PVF blob → publish seq→131）。MATCH=True/blob 206/GM ready。回滚：服务端 `docker tag servers5a3-local:pre-cycle-20261120 servers5a3-local:dev` + force-recreate + 本地 PVF `Script.pvf.bak-precycle-cf9a7e78`；配置备份 `*.bak-pre-cycle-20261120124352` |
| 2026-11-20 中午 (seq 133) | `cf9a7e78`（未变） | `74478473`(112,828,416) | **EXE 换版（用户更新本地 DNF.exe）**：`release_exe.py` 一键发布；manifest 1.2.20/seq25（只改 DNF.exe 条目）+ 新 EXE blob，两处 profile ExeSha+SecondClient 双改（白名单增至 7）、sequence 131→132(content publish→133)，MATCH=True/blob 206/登录器 1.0.24 不变/AntiCheat audit 保持。回滚备份 `*.bak-pre-exe74478473-20261120125955` |
| 2026-11-20 下午 (seq 135) | `452534c2`(129,739,873) | `74478473`（未变） | **15 个深渊相关任务改道具提交**：任务 ID `10103/10104/4391/6880/5782/5783/10105/10106/4539/10107/10108/2002/10109/10110/2900`，PVF 内 `[type]` 统一改为 `[seeking]`、`[int data]` 改为 `3037 1`（无色小晶体 ×1），条件文案追加「提交1个无色小晶体。」。**基线必须用线上实际 PVF `cf9a7e78`**（本地 `轻装更新\20261117\Script.pvf` 的 `b038d4c2` 是旧缓存，勿作基线）；PVF-CLI schema2 token patch，15 entries + 1 appended string，patch/verify 全绿。发布：`release_pvf.py --from 1 --to 6`（`reused=false`，seq 133→134）→ 停网关修 content Script.pvf Length 129739594→129739873 → 手动 up（`--channel-info` + admin secret post-fix）→ force-recreate gateway/gm → manifest 1.2.21/seq26 + PVF blob → check compatible=true → publish seq 135。验收三副本哈希一致、旧索引已删、GM ready、游戏服 0 panic/fatal、MATCH=True/blob 206。回滚：面板/配置不回滚；PVF 回滚可用服务器 `*.bak-pre-pvf-452534c2-*` / 旧 blob `cf9a7e78...` 重发 |
| 2026-11-20 下午 (seq 137) | `bdabe3298`(129,982,166) | `74478473`（未变） | **用户本地 PVF 更新发布**：发布前检查三区块与 201801 循环模式完好、15 个 `[seeking] 3037 1` 任务改动全部保留、item-details 缓存不存在（`reused=false`）。`release_pvf.py --from 1 --to 6`（seq 135→136）→ 停网关修 Length 129739873→129982166 → 手动 up（`--channel-info`/admin secret post-fix）→ force-recreate gateway/gm → manifest 1.2.22/seq27 + PVF blob → check compatible=true → publish seq 137。验收三副本一致、旧 `452534c2` 索引已删、GM ready、游戏服 0 panic/fatal、MATCH=True/blob 206。回滚：服务器 `*.bak-pre-pvf-bdabe3298-*` / 旧 blob `452534c2...` |
| 2026-11-20 晚 (seq 140) | `8624e39d`(129,680,113) | `74478473`（未变） | **用户本地 PVF 连续两次更新后重发**：期间 `7e9e8582`(129,982,167)/`b83bfe52`(129,982,215) 仅本地构建或做到步骤 1-6，均未 content publish；以最新 `8624e39d` 从头重发。`release_pvf.py` 步骤 1-6（OLD `7e9e8582`，`reused=false`/`failed=0`，seq 138→139）→ 停网关修 Length 129982166→129680113 → 手动 up（`--channel-info`/admin secret post-fix）→ force-recreate gateway/gm → manifest 1.2.23/seq28 + PVF blob → check compatible=true → publish seq 140。验收三副本一致、旧 `7e9e8582` 索引已删、GM ready、游戏服 0 ERROR/game_ports=120、MATCH=True/blob 206。回滚：服务器 `*.bak-pre-pvf-8624e39d-*` / 旧 blob `bdabe3298...` |
| 2026-11-20 晚 (seq 142) | `2eae46d3`(129,680,411) | `74478473`（未变） | **用户本地 PVF 更新发布**：`release_pvf.py` 步骤 1-6（OLD `8624e39d`，`reused=false`/`failed=0`，seq 140→141）→ 停网关修 Length 129680113→129680411 → 手动 up（`--channel-info`/admin secret post-fix）→ force-recreate gateway/gm → manifest 1.2.24/seq29 + PVF blob → check compatible=true → publish seq 142。验收三副本一致、旧 `8624e39d` 索引已删、GM ready、游戏服 0 ERROR/game_ports=120、MATCH=True/blob 206。回滚：服务器 `*.bak-pre-pvf-2eae46d3-*` / 旧 blob `8624e39d...` |
| 2026-11-20 晚 (seq 144) | `343232b6`(129,680,220) | `74478473`（未变） | **用户本地 PVF 再次更新发布**：`release_pvf.py` 步骤 1-6（OLD `2eae46d3`，`reused=false`/`failed=0`，seq 142→143）→ 停网关修 Length 129680411→129680220 → 手动 up（`--channel-info`/admin secret post-fix）→ force-recreate gateway/gm → manifest 1.2.25/seq30 + PVF blob → check compatible=true → publish seq 144。验收三副本一致、旧 `2eae46d3` 索引已删、GM ready、游戏服 0 ERROR/game_ports=120、MATCH=True/blob 206。回滚：服务器 `*.bak-pre-pvf-343232b6-*` / 旧 blob `2eae46d3...` |
| 2026-11-20 晚 (seq 146) | `f16a72d1`(129,680,425) | `7483f8ab`(112,828,416) | **每日活跃度奖励显示/数值调整（PVF+EXE 双换版）**：PVF token patch（etc/chn_actionpointsystem.etc token 1963/1977）把 `[private activity rewards]` 代币 `500/500/5500`→`200/500/1000`（道具 ID 990027401/402/403 不变）；EXE 二进制等长补丁：XUI 文字 `欢乐代币 ×500/×5500`→`×200/×1000`（50点档 ×500 不变）、`100/156点疲劳药`→`30/50/100点疲劳药`，`skinMatches()` 校验 coins 的 `jne`(VA 0x6ef3abc/文件 0x6adeabc)→6×nop（因 48B 指令块塞不下 200/500/1000 三个立即数，等长改值不可行；nop 后不校验 coins 值，其他校验保持）。先发 PVF（`release_pvf.py` 1-6，seq 144→145，`reused=false`）再补 EXE profile 双改（ExeSha256+SecondClient 白名单，不再 +1 seq）+ 游戏服 up/post-fix + force-recreate gateway/gm → manifest 1.2.26/seq31（EXE+PVF 两条目）+ 双 blob → publish seq 146。**踩坑**：`release_pvf.py` 不传 content blob，须单独 scp 新 PVF 到 `hub/content-blobs/<sha>`，否则玩家下载 404（本轮已补）。验收三副本一致、两 blob 206、GM ready、游戏服 0 ERROR/game_ports=120、MATCH=True。注意：活跃度指纹变化，当天已有记录的老角色领取报 code=3，次日 06:00 重置恢复（用户已知悉接受）。回滚：`DNF.exe.bak-pre-ap-reward` / `Script.pvf.bak-pre-ap-reward` / manifest.bak-1.2.25 / 服务器 `*.bak-pre-pvf-f16a72d1-*` |
| 2026-11-20 晚 (seq 148) | `f16a72d1`（未变） | `2551b8a7`(112,828,416) | **纯 EXE 换版：多买多送 FE_TAIL 文案**：把 `本期赠礼至第6套；后续购买不追加档位奖励。` 改为 `从第25套起面板不在显示，自动开始下一个循环，奖励自动邮件发送`。该串是 festival payload 的 UTF-16LE 写死文案（file `0x6aebe04`/VA `0x6f00e04`），新串 62B 比原 42B 长 20B 无法等长原地替换 → 改写指向：新串写入 `.fe012r` 尾空闲区 file `0x6aec260`(VA `0x6f01260`)，唯一引用 `0x6ef3866 push 0x6f00e04` 的立即数改成 `0x6f01260`（等长）；原串保留、文件大小不变。`release_exe.py` 一条命令发布（profile 双改+manifest 1.2.27/seq32+blob+publish seq 148）；verify MATCH=True/blob 206。回滚：`DNF.exe.bak-pre-festivaltail`。注意：该文案仅在 0x7f02 包 `tail==0`（无循环）分支显示，当前 201801 循环模式 `tail=25` 显示的是"第25套起：每套赠送…"分支，改后此模式下看不到。详见 `去公告工具/去公告逆向手册.md` §19 |

## 附录 C：旧「铺 9166 档循环」方案（**已废弃，勿执行**；现行循环走服务端 patch `3e4a0fe0` + 24 套循环 PVF，见 §2 末与版本台账 seq131）

需求：1-24 套奖励原样；第 25 套跳过；26→二重、27→三重…到至尊礼盒3；再跳过一套；如此循环一直下去（PVF 上限 at=10000）。

1. **读原文**：`pvf.exe read --pvf <当前 Script.pvf> --path etc/chn_cerashop_multibonus.etc --format tokens --out %TEMP%\mb_full.json`。三区块 token 结构：periods 每行 5 token（id/name(text,type6)/RepeatFrom/RepeatItem/RepeatCount）；rewards 每行 4 token（period/at/item/count），全是 type0 `payload_u32`。
2. **生成 plan**：脚本 `deploy\_pvf_build_festival_loop.py`（本次临时脚本，逻辑已写入本节）——① 定位 periods 里 201801 行（token 155-159），把后三个数值 token 改 `0/0/0`；② 按 `for at in 26..10000`（skip 当 `(at-1)%12==0`，否则 item=`490701782+((at-1)%12-1)`, count=1）追加 rewards 行到 `[/private festival rewards]` 标签前；③ 旧 token 保留 `type+payload_u32`，plan 用 schema2 + `input_sha256`(整 PVF) + `before_sha256`(entry raw_sha256)。
3. **落盘验证**：`pvf.exe patch --pvf <pvf> --plan plan.json --out candidate.pvf` → `pvf.exe verify --pvf <pvf> --plan plan.json --out candidate.pvf`（应 `changed_entries:1`、`unchanged_entries_verified:100万+`）。再用 Python 复刻 `festival.Parse` 全部约束逐档校验（at≤10000/count≤9999/无重复/RepeatFrom=0 才允许高 at）。
4. **冒烟**：本机 Go 1.27 有 SSA ICE，`go test ./internal/services/dnfbridge` 会随机崩（与改动无关）；festival 包测试 `TestPublishedSixPeriodConfigFragment` 可单独跑通。以 patch verify + Python 复刻校验为准。
5. **发布**：§3 一键脚本 `release_pvf.py --pvf <candidate>` 跑完 1-8；**注意** `deploy\PurpleMagic\gmindex\gmconfig.json` 是 gitignored，若丢失从 `数据迁移\_extracted\gmconfig_old.json` 复制，把 `pvfIndexPath`/`itemIndexPath` 改回 gmindex 目录（脚本只改 pvfPath/pvfSha256）；随后 §4 content 通道——用 `_pvf_resign_manifest.py` 基于权威 manifest 只改 Script.pvf 条目（**绝不能用本地陈旧 DNF.exe 重生成 manifest**，会破坏 EXE 配对），版本末位+1、seq+1；上传 blob（文件名=新 sha）+ manifest 入位 → `publish_content.py` → `verify_bootstrap.py` + PVF 专项校验（profile.PVF==content.PVF、blob Range 206）。
6. **回滚**：三副本有 `.bak-b038d4c2`；manifest 有 `.bak-1.2.3`；改回哈希 + sequence 再 +1 + 重启，内容通道重发旧 blob。
