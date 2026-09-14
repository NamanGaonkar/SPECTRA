@echo off
REM SPECTRA installer for Windows
REM Copies spectra.exe to %LOCALAPPDATA%\Programs\spectra and adds it to the user PATH.

setlocal
set "DEST=%LOCALAPPDATA%\Programs\spectra"
set "EXE=%~dp0spectra.exe"

if not exist "%EXE%" (
    echo [!] spectra.exe not found next to install.bat. Build it first:
    echo     go build -trimpath -ldflags="-s -w" -o spectra.exe .
    exit /b 1
)

mkdir "%DEST%" 2>nul
copy /y "%EXE%" "%DEST%\spectra.exe" >nul
if errorlevel 1 (
    echo [!] Failed to copy to "%DEST%". Close any running spectra and retry.
    exit /b 1
)

REM Add to user PATH if not already present
echo %PATH% | find /i "%DEST%" >nul
if errorlevel 1 (
    for /f "skip=2 tokens=1,2*" %%A in ('reg query HKCU\Environment /v PATH 2^>nul') do set "USERPATH=%%C"
    if defined USERPATH (
        setx PATH "%USERPATH%;%DEST%" >nul
    ) else (
        setx PATH "%DEST%" >nul
    )
    echo [+] Added "%DEST%" to your user PATH. Open a NEW terminal for it to take effect.
)

echo.
echo [+] SPECTRA installed successfully.
echo     Run it from any terminal:  spectra
echo     Verify the setup first:    spectra --check
echo.
endlocal
