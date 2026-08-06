$ErrorActionPreference = 'Stop'
$repo  = 'G:\GlassMarble'
$importPath = 'github.com/Syamchand123/GlassMarble/internal/product/ont'
$importLine = "`t$importPath"

# Production files (non-test, non-ont-package, non-testdata) that use ont. but
# are not the ont package itself.
$files = rg -l --glob '*.go' -g '!**/*_test.go' -g '!**/testdata/**' -g '!**/internal/product/ont/**' 'ont\.' $repo

foreach ($f in $files) {
    $content = Get-Content -Raw -Encoding UTF8 $f

    if ($content -match [regex]::Escape($importPath)) {
        # already imports ont
        continue
    }

    # Insert into a grouped import block after `import (`.
    $pattern = '(?m)^(\s*)import \(\s*\r?\n'
    $m = [regex]::Match($content, $pattern)
    if ($m.Success) {
        $content = $content.Insert($m.Index + $m.Length, $importLine + "`n")
    } else {
        # Single-line import: import "x" -> import (\n\t"x"\n\tont\n)
        $sm = [regex]::Match($content, '^(\s*)import ".*?"\s*$', 'Multiline')
        if ($sm.Success) {
            $group = $sm.Groups[0].Value
            $replaced = "import (`n" + $group.Trim() + "`n" + $importLine + "`n)"
            $content = $content.Remove($sm.Index, $sm.Length).Insert($sm.Index, $replaced)
        } else {
            Write-Error "no import block found in $f"
        }
    }
    Set-Content -Path $f -Value $content -Encoding UTF8
    Write-Output "added import to: $f"
}
Write-Output "import injection done"
