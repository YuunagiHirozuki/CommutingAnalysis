
#性能测试
$ErrorActionPreference = "Stop"

$BaseUrl = "http://127.0.0.1:8080"
$City = "guangzhou"
$Latitude = 23.1291
$Longitude = 113.2644
$TimeLimits = @(30, 45, 60, 90)
$Repeats = 5

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ResultDir = Join-Path $ScriptDir "results"
$OutputFile = Join-Path $ResultDir "performance.csv"
New-Item -ItemType Directory -Force -Path $ResultDir | Out-Null

Write-Host "检查后端是否运行：$BaseUrl"
try {
    Invoke-WebRequest -Uri "$BaseUrl/api/cities" -Method GET -TimeoutSec 5 -UseBasicParsing | Out-Null
} catch {
    Write-Host "无法连接后端，请先启动 Go 服务。" -ForegroundColor Red
    Write-Host "例如：go run ./cmd/server -config config/config.json"
    exit 1
}

$results = @()

foreach ($maxTime in $TimeLimits) {
    Write-Host "========================================"
    Write-Host "开始测试 maxTime = $maxTime 分钟"
    Write-Host "========================================"

    $warmupBody = @{
        city = $City
        latitude = $Latitude
        longitude = $Longitude
        maxTime = $maxTime
        includeNetwork = $false
    } | ConvertTo-Json

    try {
        Invoke-RestMethod -Uri "$BaseUrl/api/isochrone" -Method POST -ContentType "application/json" -Body $warmupBody | Out-Null
    } catch {
        Write-Host "预热请求失败：$($_.Exception.Message)" -ForegroundColor Red
        exit 1
    }

    for ($run = 1; $run -le $Repeats; $run++) {
        $body = @{
            city = $City
            latitude = $Latitude
            longitude = $Longitude
            maxTime = $maxTime
            includeNetwork = $false
        } | ConvertTo-Json

        $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
        try {
            $response = Invoke-RestMethod -Uri "$BaseUrl/api/isochrone" -Method POST -ContentType "application/json" -Body $body
            $stopwatch.Stop()

            $results += [PSCustomObject]@{
                maxTime = $maxTime
                run = $run
                computeMs = $response.computeMs
                httpMs = $stopwatch.ElapsedMilliseconds
                reachableStations = $response.reachableStations
                gridCells = $response.gridCells
                testData = $response.testData
                notice = $response.notice
            }

            Write-Host ("  Run {0}: computeMs={1}, httpMs={2}, stations={3}, gridCells={4}" -f $run, $response.computeMs, $stopwatch.ElapsedMilliseconds, $response.reachableStations, $response.gridCells)
        } catch {
            $stopwatch.Stop()
            Write-Host ("  Run {0}: 请求失败，耗时 {1} ms" -f $run, $stopwatch.ElapsedMilliseconds) -ForegroundColor Red
            Write-Host $_.Exception.Message -ForegroundColor Red
        }
    }
    Write-Host ""
}

$results | Export-Csv -Path $OutputFile -NoTypeInformation -Encoding UTF8

Write-Host "========================================"
Write-Host "测试完成"
Write-Host "结果文件：$OutputFile"
Write-Host "========================================"
Write-Host ""

$results | Group-Object maxTime | ForEach-Object {
    $group = $_.Group
    $avgCompute = ($group | Measure-Object computeMs -Average).Average
    $avgHttp = ($group | Measure-Object httpMs -Average).Average
    $avgStations = ($group | Measure-Object reachableStations -Average).Average
    $avgGrid = ($group | Measure-Object gridCells -Average).Average
    Write-Host ("{0} min: compute={1:N1} ms, http={2:N1} ms, stations={3:N1}, gridCells={4:N1}" -f $_.Name, $avgCompute, $avgHttp, $avgStations, $avgGrid)
}