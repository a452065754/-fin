@echo off
rem One-click launcher for the reportviewer preview server.
rem Edit WEBROOT below if your FineReport webroot lives elsewhere.
cd /d "%~dp0"
set "WEBROOT=D:\FineReport_11.0\webapps\webroot"
set "ADDR=127.0.0.1:8099"

if not exist "reportviewer.exe" (
  echo [ERROR] reportviewer.exe not found in %CD%
  echo Build it first: go build -o reportviewer.exe ./cmd/reportviewer
  pause
  exit /b 1
)
if not exist "%WEBROOT%\WEB-INF" (
  echo [WARNING] WEBROOT not found: %WEBROOT%
  echo Edit WEBROOT in this .bat before starting.
  pause
  exit /b 1
)

echo Starting reportviewer ...
echo Open http://127.0.0.1:8099/ in your browser.
echo Keep this window open. Press Ctrl+C or close it to stop the server.
echo.
reportviewer.exe -webroot "%WEBROOT%" -addr "%ADDR%"
echo.
echo Server stopped.
pause
