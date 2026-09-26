# 系统正确性 / 接口验证
# 前置条件：
#   go run ./cmd/server -config config/config.json

$ErrorActionPreference = "Stop"

$BaseUrl = "http://127.0.0.1:8080"
$City = "guangzhou"

# 固定测试起点：广州中心区域
$Latitude = 23.1291
$Longitude = 113.2644

$ResultDir = Join-Path $PSScriptRoot "results"
$OutputFile = Join-Path $ResultDir "correctness.csv"

New-Item -ItemType Directory -Force -Path $ResultDir | Out-Null

function Invoke-Isochrone {
    param (
        [double]$MaxTime,
        [bool]$IncludeNetwork = $false
    )

    $body = @{
        city = $City
        latitude = $Latitude
        longitude = $Longitude
        maxTime = $MaxTime
        includeNetwork = $IncludeNetwork
    } | ConvertTo-Json

    return Invoke-RestMethod `
        -Uri "$BaseUrl/api/isochrone" `
        -Method POST `
        -ContentType "application/json" `
        -Body $body
}

Write-Host "========================================"
Write-Host "等时圈正确性测试"
Write-Host "========================================"

# ------------------------------------------------------------
# 1. 检查服务
# ------------------------------------------------------------

Write-Host "[1] 检查后端..."

try {
    $health = Invoke-RestMethod `
        -Uri "$BaseUrl/api/health" `
        -Method GET `
        -TimeoutSec 5

    Write-Host "    后端连接正常"
}
catch {
    Write-Host "后端无法连接，请先启动服务。" -ForegroundColor Red
    exit 1
}

# ------------------------------------------------------------
# 2. 基本计算
# ------------------------------------------------------------

Write-Host ""
Write-Host "[2] 测试 30 / 45 / 60 / 90 分钟..."

$results = @()

$previousStations = -1
$previousTime = 0

foreach ($maxTime in @(30, 45, 60, 90)) {

    try {
        $response = Invoke-Isochrone -MaxTime $maxTime

        $passed = $true
        $reason = ""

        # 基本字段
        if ($response.city -ne $City) {
            $passed = $false
            $reason = "city 返回值错误"
        }

        if ([double]$response.maxTime -ne $maxTime) {
            $passed = $false
            $reason = "maxTime 返回值错误"
        }

        if ($response.testData -eq $true) {
            $passed = $false
            $reason = "当前使用 TEST DATA"
        }

        if ($response.reachableStations -lt 0) {
            $passed = $false
            $reason = "reachableStations < 0"
        }

        if ($response.gridCells -lt 0) {
            $passed = $false
            $reason = "gridCells < 0"
        }

        if ($null -eq $response.geojson) {
            $passed = $false
            $reason = "geojson 缺失"
        }

        # 单调性：
        # 时间预算增加，可达站点数不应该减少
        if ($previousStations -ge 0) {
            if ($response.reachableStations -lt $previousStations) {
                $passed = $false
                $reason = "时间增加后可达站点数反而减少"
            }
        }

        $results += [PSCustomObject]@{
            test = "maxTime=$maxTime"
            passed = $passed
            maxTime = $maxTime
            reachableStations = $response.reachableStations
            gridCells = $response.gridCells
            computeMs = $response.computeMs
            testData = $response.testData
            reason = $reason
        }

        if ($passed) {
            Write-Host (
                "    PASS: {0} min -> stations={1}, gridCells={2}, compute={3}ms" -f
                $maxTime,
                $response.reachableStations,
                $response.gridCells,
                $response.computeMs
            )
        }
        else {
            Write-Host (
                "    FAIL: {0} min -> {1}" -f
                $maxTime,
                $reason
            ) -ForegroundColor Red
        }

        $previousStations = $response.reachableStations
        $previousTime = $maxTime
    }
    catch {
        Write-Host (
            "    FAIL: {0} min -> 请求失败: {1}" -f
            $maxTime,
            $_.Exception.Message
        ) -ForegroundColor Red

        $results += [PSCustomObject]@{
            test = "maxTime=$maxTime"
            passed = $false
            maxTime = $maxTime
            reachableStations = -1
            gridCells = -1
            computeMs = -1
            testData = $null
            reason = $_.Exception.Message
        }
    }
}

# ------------------------------------------------------------
# 3. includeNetwork
# ------------------------------------------------------------

Write-Host ""
Write-Host "[3] 测试交通线路网络开关..."

try {
    $withoutNetwork = Invoke-Isochrone `
        -MaxTime 60 `
        -IncludeNetwork $false

    $withNetwork = Invoke-Isochrone `
        -MaxTime 60 `
        -IncludeNetwork $true

    $networkPass = (
        ($null -eq $withoutNetwork.network) -and
        ($null -ne $withNetwork.network) -and
        ($withNetwork.network.features.Count -ge 0)
    )

    if ($networkPass) {
        Write-Host "    PASS: includeNetwork=false/true 行为正常"

        $results += [PSCustomObject]@{
            test = "includeNetwork"
            passed = $true
            maxTime = 60
            reachableStations = $withNetwork.reachableStations
            gridCells = $withNetwork.gridCells
            computeMs = $withNetwork.computeMs
            testData = $withNetwork.testData
            reason = ""
        }
    }
    else {
        Write-Host "    FAIL: network 返回行为异常" -ForegroundColor Red

        $results += [PSCustomObject]@{
            test = "includeNetwork"
            passed = $false
            maxTime = 60
            reachableStations = $withNetwork.reachableStations
            gridCells = $withNetwork.gridCells
            computeMs = $withNetwork.computeMs
            testData = $withNetwork.testData
            reason = "includeNetwork 返回值异常"
        }
    }
}
catch {
    Write-Host "    FAIL: $($_.Exception.Message)" -ForegroundColor Red
}

# ------------------------------------------------------------
# 4. 输出
# ------------------------------------------------------------

$results | Export-Csv `
    -Path $OutputFile `
    -NoTypeInformation `
    -Encoding UTF8

Write-Host ""
Write-Host "========================================"

$failed = @($results | Where-Object { $_.passed -eq $false }).Count

if ($failed -eq 0) {
    Write-Host "全部系统级正确性检查通过。" -ForegroundColor Green
}
else {
    Write-Host "发现 $failed 项失败。" -ForegroundColor Red
}

Write-Host "结果：$OutputFile"
Write-Host "========================================"

if ($failed -gt 0) {
    exit 1
}