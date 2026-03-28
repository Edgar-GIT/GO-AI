@echo off
setlocal
set SCRIPT_DIR=%~dp0
set INSTALLER=%SCRIPT_DIR%installer_gui.py

if not exist "%INSTALLER%" (
  echo Missing installer payload: %INSTALLER%
  pause
  exit /b 1
)

where pyw >nul 2>nul
if %errorlevel%==0 (
  pyw -3 "%INSTALLER%"
  exit /b %errorlevel%
)

where py >nul 2>nul
if %errorlevel%==0 (
  py -3 "%INSTALLER%"
  exit /b %errorlevel%
)

where pythonw >nul 2>nul
if %errorlevel%==0 (
  pythonw "%INSTALLER%"
  exit /b %errorlevel%
)

where python >nul 2>nul
if %errorlevel%==0 (
  python "%INSTALLER%"
  exit /b %errorlevel%
)

echo Python 3 is required to run the Gopher AI installer.
pause
exit /b 1
