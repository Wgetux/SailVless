# Renders the app icon (sail on a gradient circle) into <size>/sailvless.png.
# Run on Windows: .\make-icons.ps1  (output is committed; rerun only to change the design)
Add-Type -AssemblyName System.Drawing
$ErrorActionPreference = 'Stop'

foreach ($size in 86, 108, 128, 172) {
    $bmp = New-Object System.Drawing.Bitmap $size, $size
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = 'AntiAlias'
    $g.Clear([System.Drawing.Color]::Transparent)
    $s = $size / 100.0

    $rect = New-Object System.Drawing.RectangleF (2 * $s), (2 * $s), (96 * $s), (96 * $s)
    $grad = New-Object System.Drawing.Drawing2D.LinearGradientBrush $rect,
        ([System.Drawing.Color]::FromArgb(255, 21, 101, 192)), ([System.Drawing.Color]::FromArgb(255, 0, 172, 193)), 45.0
    $g.FillEllipse($grad, $rect)

    $white = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::White)
    $soft = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(200, 255, 255, 255))
    # main sail and jib
    $g.FillPolygon($white, [System.Drawing.PointF[]]@(
        (New-Object System.Drawing.PointF (50 * $s), (18 * $s)),
        (New-Object System.Drawing.PointF (50 * $s), (66 * $s)),
        (New-Object System.Drawing.PointF (24 * $s), (66 * $s))))
    $g.FillPolygon($soft, [System.Drawing.PointF[]]@(
        (New-Object System.Drawing.PointF (54 * $s), (28 * $s)),
        (New-Object System.Drawing.PointF (54 * $s), (66 * $s)),
        (New-Object System.Drawing.PointF (74 * $s), (66 * $s))))
    # hull
    $g.FillPolygon($white, [System.Drawing.PointF[]]@(
        (New-Object System.Drawing.PointF (20 * $s), (71 * $s)),
        (New-Object System.Drawing.PointF (80 * $s), (71 * $s)),
        (New-Object System.Drawing.PointF (70 * $s), (81 * $s)),
        (New-Object System.Drawing.PointF (30 * $s), (81 * $s))))

    $dir = Join-Path $PSScriptRoot "${size}x${size}"
    New-Item -ItemType Directory -Force $dir | Out-Null
    $bmp.Save((Join-Path $dir 'sailvless.png'), [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
}
