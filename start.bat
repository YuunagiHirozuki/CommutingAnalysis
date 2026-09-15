@echo off
title CommutingAnalysis

echo Starting backend...
start "Backend" cmd /k "cd /d %~dp0backend && go run ./cmd/server -config config/config.json"

echo Starting frontend...
start "Frontend" cmd /k "cd /d %~dp0frontend && pnpm run dev"

echo.
echo Backend and frontend are starting...
pause