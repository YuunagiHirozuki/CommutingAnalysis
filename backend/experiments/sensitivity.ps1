# 换乘惩罚参数敏感性实验
# 注意：
#   本脚本会自动启动和关闭 Go 后端。
#   请确保运行脚本前没有其他程序占用 8080 端口。

$ErrorActionPreference = "Stop"

# ------------------------------------------------------------
# 基本配置
# ------------------------------------------------------------

$ConfigPath = Join-Path $PSScriptRoot "..\config\config.json"
$ConfigPath = (Resolve-Path $ConfigPath).Path

$BackendPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

$BaseUrl = "http://127.0.0.1:8080"
$HealthUrl = "$BaseUrl/api/health"
$IsochroneUrl = "$BaseUrl/api/isochrone"

$City = "guangzhou"

$Latitude = 23.1291
$Longitude = 113.2644

$MaxTime = 60

$PenaltyValues = @(0, 5, 10, 15)

$ResultDir = Join-Path $PSScriptRoot "results"
$OutputFile = Join-Path $ResultDir "sensitivity.csv"

New-Item -ItemType Directory -Force -Path $ResultDir | Out-Null

# ------------------------------------------------------------
# 函数：停止占用 8080 端口的进程
# ------------------------------------------------------------

function Stop-Backend {

    Write-Host "检查 8080 端口..."

    try {

        $connections = Get-NetTCPConnection `
            -LocalPort 8080 `
            -State Listen `
            -ErrorAction SilentlyContinue

        if ($connections) {

            $pids = $connections |
                Select-Object -ExpandProperty OwningProcess -Unique

            foreach ($processId in $pids) {

                Write-Host "发现后端进程 PID=$processId，正在停止..."

                # /T：同时结束子进程
                # /F：强制结束
                taskkill /PID $processId /T /F | Out-Null
            }

            Start-Sleep -Seconds 1
        }
    }
    catch {

        Write-Host "停止后端时出现异常：$($_.Exception.Message)"
    }
}


# ------------------------------------------------------------
# 函数：启动 Go 后端
# ------------------------------------------------------------

function Start-Backend {

    Write-Host "正在启动 Go 后端..."

    # 使用独立 PowerShell 窗口运行 go run
    $process = Start-Process `
        -FilePath "powershell.exe" `
        -ArgumentList "-NoProfile", "-Command", "Set-Location '$BackendPath'; go run ./cmd/server" `
        -PassThru `
        -WindowStyle Hidden

    Write-Host "Go 启动进程 PID=$($process.Id)"

    return $process
}

# ------------------------------------------------------------
# 函数：等待后端启动
# ------------------------------------------------------------

function Wait-BackendReady {

    Write-Host "等待后端启动..."

    for ($i = 1; $i -le 30; $i++) {

        try {

            Invoke-RestMethod `
                -Uri $HealthUrl `
                -Method GET `
                -TimeoutSec 2 | Out-Null

            Write-Host "后端已启动。"

            return $true
        }
        catch {

            Write-Host "等待后端... ($i/30)"

            Start-Sleep -Seconds 1
        }
    }

    return $false
}

# ------------------------------------------------------------
# 读取原始配置
# ------------------------------------------------------------

$config = Get-Content `
    -Path $ConfigPath `
    -Raw |
    ConvertFrom-Json

$OriginalPenalty = $config.params.transferPenaltyMin

Write-Host ""
Write-Host "========================================"
Write-Host "敏感性实验"
Write-Host "========================================"
Write-Host "原始 transferPenaltyMin = $OriginalPenalty"
Write-Host "实验参数：$($PenaltyValues -join ', ')"
Write-Host "最大时间：$MaxTime 分钟"
Write-Host "========================================"

$results = @()

try {

    foreach ($penalty in $PenaltyValues) {

        Write-Host ""
        Write-Host "========================================"
        Write-Host "transferPenaltyMin = $penalty"
        Write-Host "========================================"

        # ----------------------------------------------------
        # 1. 确保旧后端已经关闭
        # ----------------------------------------------------

        Stop-Backend

        # ----------------------------------------------------
        # 2. 修改配置
        # ----------------------------------------------------

        $config.params.transferPenaltyMin = $penalty

        $config |
            ConvertTo-Json -Depth 20 |
            Set-Content `
                -Path $ConfigPath `
                -Encoding UTF8

        Write-Host "配置已修改：transferPenaltyMin = $penalty"

        # ----------------------------------------------------
        # 3. 启动新的 Go 后端
        # ----------------------------------------------------

        $backendProcess = Start-Backend

        # ----------------------------------------------------
        # 4. 等待后端启动
        # ----------------------------------------------------

        $ready = Wait-BackendReady

        if (-not $ready) {

            Write-Host "后端启动失败，无法继续实验。" -ForegroundColor Red

            Stop-Backend

            exit 1
        }

        # ----------------------------------------------------
        # 5. 构造等时圈请求
        # ----------------------------------------------------

        $body = @{
            city = $City
            latitude = $Latitude
            longitude = $Longitude
            maxTime = $MaxTime
            includeNetwork = $false
        } | ConvertTo-Json

        # ----------------------------------------------------
        # 6. 调用等时圈 API
        # ----------------------------------------------------

        $response = Invoke-RestMethod `
            -Uri $IsochroneUrl `
            -Method POST `
            -ContentType "application/json" `
            -Body $body

        # ----------------------------------------------------
        # 7. 保存实验结果
        # ----------------------------------------------------

        $results += [PSCustomObject]@{

            transferPenaltyMin = $penalty

            maxTime = $MaxTime

            reachableStations = $response.reachableStations

            gridCells = $response.gridCells

            computeMs = $response.computeMs

            testData = $response.testData
        }

        Write-Host ""
        Write-Host (
            "Penalty={0}: stations={1}, gridCells={2}, compute={3}ms" -f `
            $penalty,
            $response.reachableStations,
            $response.gridCells,
            $response.computeMs
        )

        # ----------------------------------------------------
        # 8. 关闭本轮后端
        # ----------------------------------------------------

        Stop-Backend

        Write-Host "本轮实验完成。"
    }
}
finally {

    # --------------------------------------------------------
    # 无论实验是否成功，都恢复原始配置
    # --------------------------------------------------------

    Stop-Backend

    $config.params.transferPenaltyMin = $OriginalPenalty

    $config |
        ConvertTo-Json -Depth 20 |
        Set-Content `
            -Path $ConfigPath `
            -Encoding UTF8

    Write-Host ""
    Write-Host "========================================"
    Write-Host "已恢复原始配置"
    Write-Host "transferPenaltyMin = $OriginalPenalty"
    Write-Host "========================================"
}

# ------------------------------------------------------------
# 保存 CSV
# ------------------------------------------------------------

$results |
    Export-Csv `
        -Path $OutputFile `
        -NoTypeInformation `
        -Encoding UTF8

# ------------------------------------------------------------
# 输出结果
# ------------------------------------------------------------

Write-Host ""
Write-Host "========================================"
Write-Host "敏感性实验完成"
Write-Host "结果：$OutputFile"
Write-Host "========================================"

$results | Format-Table -AutoSize