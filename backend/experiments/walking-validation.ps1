# 步行时间模型误差验证
# 验证：Haversine 直线距离 + 5 km/h 与实际步行时间之间的偏差。
# 本测试不修改后端配置。

$ErrorActionPreference = "Stop"

$WalkingSpeedKmh = 5.0

$ResultDir = Join-Path $PSScriptRoot "results"
$OutputFile = Join-Path $ResultDir "walking-validation.csv"

New-Item -ItemType Directory -Force -Path $ResultDir | Out-Null

# ------------------------------------------------------------
# 在这里填写实际测量数据
# distanceM = 起点到站点的直线距离
# actualMin = 实际步行耗时
# ------------------------------------------------------------

$Samples = @(
    @{
        name = "样本1"
        distanceM = 300
        actualMin = 4.5
    },
    @{
        name = "样本2"
        distanceM = 500
        actualMin = 7.0
    },
    @{
        name = "样本3"
        distanceM = 800
        actualMin = 11.0
    },
    @{
        name = "样本4"
        distanceM = 1000
        actualMin = 14.0
    },
    @{
        name = "样本5"
        distanceM = 1500
        actualMin = 21.0
    }
)

$results = @()

foreach ($sample in $Samples) {

    $distanceM = [double]$sample.distanceM
    $actualMin = [double]$sample.actualMin

    $modelMin =
        $distanceM /
        ($WalkingSpeedKmh * 1000 / 60)

    $errorMin = $actualMin - $modelMin

    $errorPercent = 0

    if ($actualMin -gt 0) {
        $errorPercent =
            $errorMin / $actualMin * 100
    }

    $results += [PSCustomObject]@{
        name = $sample.name
        distanceM = $distanceM
        modelMin = [math]::Round($modelMin, 2)
        actualMin = $actualMin
        errorMin = [math]::Round($errorMin, 2)
        errorPercent = [math]::Round($errorPercent, 2)
    }
}

$results | Export-Csv `
    -Path $OutputFile `
    -NoTypeInformation `
    -Encoding UTF8

Write-Host "========================================"
Write-Host "步行时间模型验证"
Write-Host "========================================"

$results | Format-Table -AutoSize

$avgError =
    ($results | Measure-Object errorMin -Average).Average

$avgAbsError =
    (
        $results |
        ForEach-Object { [math]::Abs($_.errorMin) } |
        Measure-Object -Average
    ).Average

Write-Host ""
Write-Host ("平均误差：{0:N2} 分钟" -f $avgError)
Write-Host ("平均绝对误差：{0:N2} 分钟" -f $avgAbsError)

Write-Host ""
Write-Host "结果：$OutputFile"