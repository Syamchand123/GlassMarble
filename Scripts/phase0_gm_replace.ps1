$ErrorActionPreference = 'Stop'
$repo = 'G:\GlassMarble'

# Build TERM -> Go-identifier map from the generated ont.go so replacements stay in sync.
$ontFile = Join-Path $repo 'internal\product\ont\ont.go'
$termToName = @{}
foreach ($line in Get-Content $ontFile) {
    if ($line -match '^\s*(Pred\w+)\s*=\s*"gm:([A-Za-z0-9_]+)"') {
        $termToName[$matches[2]] = $matches[1]
    }
}
Write-Output "loaded $($termToName.Count) ont predicates"

# Production (non-test) files that currently contain gm:/ext: literals,
# excluding the ont package itself and testdata.
$files = rg -l --glob '*.go' -g '!**/*_test.go' -g '!**/testdata/**' -g '!**/internal/product/ont/**' 'gm:' $repo
$extFiles = rg -l --glob '*.go' -g '!**/*_test.go' -g '!**/testdata/**' -g '!**/internal/product/ont/**' 'ext:' $repo
$all = ($files + $extFiles) | Sort-Object -Unique
Write-Output "production files to touch: $($all.Count)"

foreach ($f in $all) {
    $original = Get-Content -Raw -Encoding UTF8 $f
    $content = $original

    # 1) pure quoted "gm:Term" -> ont.PredName
    $content = [regex]::Replace($content, '"gm:([A-Za-z0-9_]+)"', {
        param($m)
        $term = $m.Groups[1].Value
        if ($termToName.ContainsKey($term)) {
            return "ont.$($termToName[$term])"
        }
        return $m.Value
    })

    # 2) bare quoted "gm:" -> ont.PrefixGM
    $content = [regex]::Replace($content, '"gm:"', { param($m); 'ont.PrefixGM' })

    # 3) quoted "ext:rest" -> ont.PrefixExt + "rest"
    $content = [regex]::Replace($content, '"ext:([^"]*)"', {
        param($m)
        $rest = $m.Groups[1].Value
        if ($rest -eq '') { return 'ont.PrefixExt' }
        return "ont.PrefixExt + `"$rest`""
    })

    if ($content -ceq $original) { continue }

    Set-Content -Path $f -Value $content -Encoding UTF8
    Write-Output "updated: $f"
}
Write-Output ("bulk replacement pass complete ({0} files scanned)" -f $all.Count)