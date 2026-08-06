# TSIngest

Live Media Mesh 的生产级多路 SRT 收录管理系统。每次手动录制生成一个完整 TS 文件；TS 正常结束后可以手动或自动生成 H.264 / H.265 MP4。

## 功能

- Listener 与 Caller 两种 SRT 接入模式，最多 64 路并发收录。
- 完整 TS 母版，不分段、不限制单次时长。
- 多音轨保留；H.264 与 HEVC/H.265 视频直接转封装，非 MP4 兼容音频逐轨转 AAC。
- 单管理员会话认证、SRT 口令加密、后台命令持久化。
- FFmpeg 进程组优雅停止、Worker 重启恢复、磁盘软硬水位保护。
- 中文响应式管理界面、SSE 实时状态、MP4 预览及 Range 下载。
- `linux/amd64` 与 `linux/arm64` Docker 镜像。
- FFmpeg 固定为 Debian `5.1.9-0+deb12u1`，镜像构建会校验 SRT、MPEG-TS、MP4、H.264、HEVC 和 AAC 能力。

## 启动

1. 准备配置和录制目录：

   ```bash
   ./scripts/configure-env.sh
   ```

   也可以非交互生成生产配置：

   ```bash
   ./scripts/configure-env.sh \
     --non-interactive \
     --recordings-path /data/tsingest/recordings \
     --admin-password 'replace-with-a-strong-admin-password'
   ```

   Linux 主机需要确保 UID `10001` 对录制目录有读写权限：

   ```bash
   sudo mkdir -p /data/tsingest/recordings
   sudo chown -R 10001:10001 /data/tsingest/recordings
   ```

2. 推荐：直接拉取预构建镜像并启动：

   ```bash
   # 如果使用 GitHub Container Registry 发布镜像
   sed -i '/^TSINGEST_IMAGE=/d' .env
   echo 'TSINGEST_IMAGE=ghcr.io/mophiest/tsingest:0.1.0' >> .env
   ./scripts/deploy-online.sh --image ghcr.io/mophiest/tsingest:0.1.0
   ```

   这种方式不会在生产机编译 Go、安装 npm 依赖或执行 apt 安装 FFmpeg，最适合正式部署。

3. 源码构建并启动：

   ```bash
   docker compose build
   docker compose up -d
   ```

4. 访问 `http://服务器地址:8080`，使用 `.env` 中的管理员账号登录。

Listener 使用 UDP 端口 `9000–9099`。请在主机防火墙中仅向需要的发送端开放对应 UDP 端口。

如果启动时报 `could not find an available, non-overlapping IPv4 address pool`，说明 Docker 默认网段和服务器现有网络冲突。修改 `.env` 中的 `TSINGEST_DOCKER_SUBNET`，例如：

```env
TSINGEST_DOCKER_SUBNET=172.31.240.0/24
```

如果仍冲突，可以换成生产网络中未使用的小网段，例如 `172.30.240.0/24` 或 `10.250.0.0/24`。

如果 FFmpeg/SRT 报 `pthread_create failed with 1`，通常是 Docker seccomp 与系统线程创建兼容性问题。默认 `.env` 使用：

```env
TSINGEST_SECCOMP_PROFILE=unconfined
```

该配置只应用于运行 FFmpeg 的 `worker` 和测试发送容器。升级 Docker/libseccomp 后，如需收紧可改为 `default` 并重新 `docker compose up -d`。

## 推荐生产发布方式

正式环境建议不要在生产机 `docker compose build`。推荐发布流程：

1. 在 GitHub Actions 或构建机生成镜像。
2. 推送为 `ghcr.io/mophiest/tsingest:<版本号>`。
3. 生产机只执行：

   ```bash
   git pull
   ./scripts/deploy-online.sh --image ghcr.io/mophiest/tsingest:0.1.0
   ```

这样可以避免现场机器受到 Go 代理、Debian 源、npm 网络、Docker BuildKit 版本差异影响。

如需发布新版本，在仓库创建 tag，例如 `v0.1.0`，GitHub Actions 默认构建并推送 `linux/amd64` 镜像，并启用远程构建缓存。当前生产服务器是 `x86_64`，使用这个默认值即可。

如以后确实需要 ARM 服务器，可以手动运行 `release-images` workflow，并把 platforms 改为 `linux/amd64,linux/arm64`。
如果 GHCR 包保持私有，生产机需要先执行 `docker login ghcr.io`；如果设为 public，则可以直接拉取。

## 离线镜像包部署

如果生产环境不能联网，或者希望在开发机打好镜像再带到现场：

1. 在构建机生成离线包：

   ```bash
   APP_VERSION=0.1.0 ./scripts/package-offline.sh --platform linux/amd64
   ```

   输出文件：

   ```text
   release/tsingest-0.1.0-linux-amd64-offline.tar.gz
   ```

   这个包包含：

   - `tsingest:0.1.0` 应用镜像
   - `postgres:17-alpine` 数据库镜像
   - `compose.yaml`
   - `.env.example`
   - 配置脚本和加载脚本

2. 拷贝到生产服务器并解压：

   ```bash
   tar -xzf tsingest-0.1.0-linux-amd64-offline.tar.gz
   cd tsingest-0.1.0-linux-amd64
   ```

3. 加载镜像并生成配置：

   ```bash
   ./load-offline.sh
   ./scripts/configure-env.sh
   sudo mkdir -p /data/tsingest/recordings
   sudo chown -R 10001:10001 /data/tsingest/recordings
   ```

4. 启动：

   ```bash
   docker compose up -d
   ```

后续升级同样重新生成离线包，到生产机执行 `./load-offline.sh` 后再 `docker compose up -d`。

生产机如果是 `x86_64`，使用 `linux/amd64`；如果是 `aarch64` 或 ARM 服务器，才需要单独构建 `linux/arm64`。可以在生产机执行 `uname -m` 确认。

## 源码直跑调试

现场调试时可以只用 Docker 跑 Postgres，Web 和 Worker 直接从源码启动：

```bash
./scripts/configure-env.sh
scripts/dev-run.sh
```

脚本会：

- 用 `compose.dev.yaml` 把 Postgres 绑定到宿主机 `127.0.0.1:5432`
- 执行 `npm ci && npm run build`
- 将前端产物复制到 `internal/ui/dist`
- 编译 `.dev/tsingest`
- 同时启动 `web` 和 `worker`

只启动单个角色：

```bash
scripts/dev-run.sh --role web
scripts/dev-run.sh --role worker --skip-frontend --no-postgres
```

如果本机 `5432` 被占用：

```bash
POSTGRES_PORT=15432 scripts/dev-run.sh
```

## 使用测试素材

在界面添加一个 Listener 流，端口设为 `9000`，点击“开始录制”，随后执行：

```bash
docker compose --profile tools run --rm test-sender
```

停止测试发送端会触发 `source_disconnect`，系统仍会完成并保留 TS。也可以先在界面点击“停止录制”，由 Worker 向 FFmpeg 发送 `SIGINT` 并完成文件。

## 录制状态判定

- `等待输入`：FFmpeg 已启动，但还没有检测到持续增长的媒体输出。
- `正在录制`：至少连续两次检测到 TS 字节或媒体时码增长；进度约每秒更新。
- `录制停滞`：界面检测到最近媒体进度超过健康窗口未更新，会以黄色告警显示。
- `正在收尾`：输入停止，Worker 正在执行 ffprobe 校验和文件完成操作。
- `已完成`：TS 可被 ffprobe 解析、时长与文件大小有效，并已从 `.part.ts` 原子重命名为 `.ts`。
- `异常`：未收到媒体、进程错误、磁盘错误或最终文件校验失败。

收到首个媒体后，如果 TS 大小和媒体时码在该通道的“无数据超时”内均不再增长，Worker 会结束本次录制并按 `source_disconnect` 收尾。界面中的音轨数量只来自录制结束后的 ffprobe 结果，录制过程中不会预设音轨数。

## 生产部署提示

- 64 路 × 20 Mbps 的峰值写入约为 160 MB/s，部署前必须对目标数据盘做持续写入测试。
- 默认软水位为剩余 `10%` 或 `100 GiB`，硬水位为剩余 `5%` 或 `20 GiB`。
- 内网部署默认使用 HTTP。如由 HTTPS 反向代理暴露，请设置 `TSINGEST_PUBLIC_URL` 和 `TSINGEST_COOKIE_SECURE=true`。
- Web 容器只读挂载录像目录；所有写入和删除均由 Worker 执行。
- 系统不会自动删除旧录像，磁盘达到硬水位时会优雅停止活动录制。

## 常用检查

```bash
docker compose ps
docker compose logs -f web worker
make unit
make package-offline
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/metrics
```
