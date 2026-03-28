@ECHO OFF
SETLOCAL
SET APP_HOME=%~dp0
IF DEFINED JAVA_HOME (
  SET JAVA_CMD=%JAVA_HOME%\bin\java.exe
) ELSE (
  SET JAVA_CMD=java.exe
)
"%JAVA_CMD%" -classpath "%APP_HOME%gradle\wrapper\gradle-wrapper.jar" org.gradle.wrapper.GradleWrapperMain %*
