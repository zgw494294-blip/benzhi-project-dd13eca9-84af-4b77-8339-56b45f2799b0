# BENZHI_README

## 项目说明
- 项目：benzhi-project-dd13eca9-84af-4b77-8339-56b45f2799b0
- 项目用途：DoseLedger CLI with atomic versioned JSON persistence, complete or attention reports, and bounded smoke workflow.
- Go 工具链：`golang:1.22.0`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run . smoke
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-dd13eca9-84af-4b77-8339-56b45f2799b0-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-dd13eca9-84af-4b77-8339-56b45f2799b0-arm64 linux/arm64
docker run -it benzhi-project-dd13eca9-84af-4b77-8339-56b45f2799b0-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run . smoke`
