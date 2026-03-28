#!/usr/bin/env python3

import os
import queue
import shutil
import subprocess
import sys
import threading
import traceback
from pathlib import Path

import tkinter as tk
from tkinter import filedialog, messagebox, ttk
from tkinter.scrolledtext import ScrolledText

try:
    import winreg
except ImportError:
    winreg = None


APP_NAME = "Gopher AI"
WINDOW_TITLE = "Gopher AI Setup"


class InstallerWizard:
    def __init__(self):
        self.is_windows = os.name == "nt"
        self.script_dir = Path(__file__).resolve().parent
        self.binary_name = "gopher-ai.exe" if self.is_windows else "gopher-ai"
        self.required_files = {
            "binary": self.script_dir / self.binary_name,
            "icon": self.script_dir / "ai.ico",
            "trainer": self.script_dir / "train_lora.py",
            "banner": self.script_dir / "installer_banner.png",
        }
        self.install_queue = queue.Queue()
        self.installing = False
        self.install_failed = False
        self.install_path = None
        self.warnings = []

        self.root = tk.Tk()
        self.root.title(WINDOW_TITLE)
        self.root.geometry("920x560")
        self.root.minsize(920, 560)
        self.root.maxsize(920, 560)
        self.root.configure(bg="#eef3fb")
        self.root.protocol("WM_DELETE_WINDOW", self.on_close)

        try:
            self.root.iconbitmap(str(self.required_files["icon"]))
        except Exception:
            pass

        self.install_dir_var = tk.StringVar(value=self.default_install_dir())
        self.add_path_var = tk.BooleanVar(value=True)
        self.desktop_var = tk.BooleanVar(value=True)
        self.python_var = tk.BooleanVar(value=False)
        self.launch_var = tk.BooleanVar(value=True)
        self.header_title = tk.StringVar()
        self.header_subtitle = tk.StringVar()
        self.status_var = tk.StringVar(value="Ready to install.")
        self.finish_var = tk.StringVar(value="")

        self.pages = []
        self.page_index = 0
        self.progress_total = 1

        self.configure_style()
        self.build_shell()
        self.ensure_payload()
        self.show_page(0)
        self.root.after(100, self.poll_queue)

    def default_install_dir(self):
        if self.is_windows:
            return str(Path(os.environ.get("LOCALAPPDATA", str(Path.home() / "AppData/Local"))) / "Programs" / APP_NAME)
        return str(Path.home() / ".local" / "opt" / "gopher-ai")

    def configure_style(self):
        style = ttk.Style(self.root)
        if "clam" in style.theme_names():
            style.theme_use("clam")
        style.configure("Installer.TFrame", background="#eef3fb")
        style.configure("Panel.TFrame", background="#07152d")
        style.configure("Body.TLabel", background="#eef3fb", foreground="#18314f", font=("Segoe UI", 10))
        style.configure("Header.TLabel", background="#eef3fb", foreground="#0a2340", font=("Segoe UI", 20, "bold"))
        style.configure("Subheader.TLabel", background="#eef3fb", foreground="#4b6583", font=("Segoe UI", 11))
        style.configure("Title.TLabel", background="#eef3fb", foreground="#0f2747", font=("Segoe UI", 16, "bold"))
        style.configure("Accent.TButton", font=("Segoe UI", 10, "bold"), padding=(20, 10))
        style.configure("Muted.TButton", font=("Segoe UI", 10), padding=(18, 10))
        style.configure("Installer.TCheckbutton", background="#eef3fb", foreground="#18314f", font=("Segoe UI", 10))
        style.map("Installer.TCheckbutton", background=[("active", "#eef3fb")])
        style.configure("Installer.Horizontal.TProgressbar", troughcolor="#d8e6f7", background="#1f7aff", bordercolor="#d8e6f7", lightcolor="#1f7aff", darkcolor="#1f7aff")

    def build_shell(self):
        outer = ttk.Frame(self.root, style="Installer.TFrame", padding=0)
        outer.pack(fill="both", expand=True)

        left = ttk.Frame(outer, style="Panel.TFrame", width=280)
        left.pack(side="left", fill="y")
        left.pack_propagate(False)

        banner_path = self.required_files["banner"]
        self.banner_image = None
        if banner_path.exists():
            try:
                self.banner_image = tk.PhotoImage(file=str(banner_path))
            except Exception:
                self.banner_image = None

        if self.banner_image:
            banner = tk.Label(left, image=self.banner_image, bg="#07152d", bd=0, highlightthickness=0)
            banner.pack(fill="both", expand=True)
        else:
            fallback = tk.Label(left, text=APP_NAME, fg="#eef9ff", bg="#07152d", font=("Segoe UI", 24, "bold"))
            fallback.pack(fill="both", expand=True)

        right = ttk.Frame(outer, style="Installer.TFrame", padding=(28, 26, 28, 24))
        right.pack(side="right", fill="both", expand=True)

        header = ttk.Frame(right, style="Installer.TFrame")
        header.pack(fill="x")

        ttk.Label(header, textvariable=self.header_title, style="Header.TLabel").pack(anchor="w")
        ttk.Label(header, textvariable=self.header_subtitle, style="Subheader.TLabel", wraplength=560).pack(anchor="w", pady=(6, 0))

        self.content = ttk.Frame(right, style="Installer.TFrame")
        self.content.pack(fill="both", expand=True, pady=(24, 0))

        self.pages = [
            self.build_welcome_page(),
            self.build_options_page(),
            self.build_install_page(),
            self.build_finish_page(),
        ]

        footer = ttk.Frame(right, style="Installer.TFrame")
        footer.pack(fill="x", pady=(18, 0))

        ttk.Separator(footer, orient="horizontal").pack(fill="x", pady=(0, 16))

        buttons = ttk.Frame(footer, style="Installer.TFrame")
        buttons.pack(fill="x")

        self.back_button = ttk.Button(buttons, text="< Back", style="Muted.TButton", command=self.go_back)
        self.back_button.pack(side="right")

        self.next_button = ttk.Button(buttons, text="Next >", style="Accent.TButton", command=self.go_next)
        self.next_button.pack(side="right", padx=(0, 12))

        self.cancel_button = ttk.Button(buttons, text="Cancel", style="Muted.TButton", command=self.on_close)
        self.cancel_button.pack(side="right", padx=(0, 12))

    def build_welcome_page(self):
        page = ttk.Frame(self.content, style="Installer.TFrame")
        ttk.Label(page, text="Welcome to the Gopher AI Setup Wizard", style="Title.TLabel").pack(anchor="w")
        ttk.Label(
            page,
            text=(
                "This installer sets up Gopher AI for the current user only.\n\n"
                "It keeps the install in your user space, avoids administrator prompts by default, and lets you choose whether to add the app to PATH, create a desktop shortcut, and install Python training packages."
            ),
            style="Body.TLabel",
            wraplength=560,
            justify="left",
        ).pack(anchor="w", pady=(14, 0))
        ttk.Label(
            page,
            text=(
                "You can change the installation folder on the next page.\n\n"
                "The default chat model inside the app stays set to Gopher-AI."
            ),
            style="Body.TLabel",
            wraplength=560,
            justify="left",
        ).pack(anchor="w", pady=(18, 0))
        return page

    def build_options_page(self):
        page = ttk.Frame(self.content, style="Installer.TFrame")

        ttk.Label(page, text="Choose where Gopher AI should be installed", style="Title.TLabel").pack(anchor="w")

        ttk.Label(page, text="Installation folder", style="Body.TLabel").pack(anchor="w", pady=(18, 6))

        directory_row = ttk.Frame(page, style="Installer.TFrame")
        directory_row.pack(fill="x")

        self.directory_entry = ttk.Entry(directory_row, textvariable=self.install_dir_var, font=("Segoe UI", 10))
        self.directory_entry.pack(side="left", fill="x", expand=True)

        ttk.Button(directory_row, text="Browse...", style="Muted.TButton", command=self.browse_directory).pack(side="left", padx=(10, 0))

        ttk.Label(
            page,
            text="These options stay in user scope and do not need administrator access.",
            style="Subheader.TLabel",
            wraplength=560,
        ).pack(anchor="w", pady=(14, 18))

        ttk.Checkbutton(page, text="Add Gopher AI to PATH", variable=self.add_path_var, style="Installer.TCheckbutton").pack(anchor="w", pady=4)
        ttk.Checkbutton(page, text="Create desktop shortcut", variable=self.desktop_var, style="Installer.TCheckbutton").pack(anchor="w", pady=4)
        ttk.Checkbutton(page, text="Install Python training packages", variable=self.python_var, style="Installer.TCheckbutton").pack(anchor="w", pady=4)
        ttk.Checkbutton(page, text="Launch Gopher AI after installation", variable=self.launch_var, style="Installer.TCheckbutton").pack(anchor="w", pady=4)

        ttk.Label(
            page,
            text="Python training packages include torch, transformers, and peft.",
            style="Subheader.TLabel",
            wraplength=560,
        ).pack(anchor="w", pady=(18, 0))

        action_row = ttk.Frame(page, style="Installer.TFrame")
        action_row.pack(fill="x", pady=(22, 0))

        ttk.Button(action_row, text="< Back", style="Muted.TButton", command=self.go_back).pack(side="right")
        ttk.Button(action_row, text="Install Now", style="Accent.TButton", command=self.start_install).pack(side="right", padx=(0, 12))
        return page

    def build_install_page(self):
        page = ttk.Frame(self.content, style="Installer.TFrame")

        ttk.Label(page, text="Installing Gopher AI", style="Title.TLabel").pack(anchor="w")
        ttk.Label(page, textvariable=self.status_var, style="Subheader.TLabel", wraplength=560).pack(anchor="w", pady=(10, 14))

        self.progress = ttk.Progressbar(page, style="Installer.Horizontal.TProgressbar", mode="determinate", maximum=100, value=0)
        self.progress.pack(fill="x")

        self.log_box = ScrolledText(page, height=16, wrap="word", font=("Cascadia Mono", 9))
        self.log_box.pack(fill="both", expand=True, pady=(18, 0))
        self.log_box.configure(state="disabled")

        return page

    def build_finish_page(self):
        page = ttk.Frame(self.content, style="Installer.TFrame")

        ttk.Label(page, text="Gopher AI is ready", style="Title.TLabel").pack(anchor="w")
        ttk.Label(page, textvariable=self.finish_var, style="Body.TLabel", wraplength=560, justify="left").pack(anchor="w", pady=(14, 0))

        self.finish_path_label = ttk.Label(page, text="", style="Subheader.TLabel", wraplength=560, justify="left")
        self.finish_path_label.pack(anchor="w", pady=(18, 0))

        return page

    def ensure_payload(self):
        missing = [str(path) for path in self.required_files.values() if not path.exists()]
        if missing:
            messagebox.showerror(WINDOW_TITLE, "The installer package is incomplete.\n\nMissing files:\n" + "\n".join(missing), parent=self.root)
            raise SystemExit(1)

    def browse_directory(self):
        chosen = filedialog.askdirectory(
            parent=self.root,
            initialdir=self.install_dir_var.get() or self.default_install_dir(),
            title="Choose installation folder",
            mustexist=False,
        )
        if chosen:
            self.install_dir_var.set(chosen)
            self.root.lift()
            self.root.focus_force()

    def show_page(self, index):
        titles = [
            ("Welcome", "A simple user-space setup wizard for Gopher AI."),
            ("Install options", "Choose the folder and the optional setup steps."),
            ("Installing", "Please wait while Gopher AI is being installed."),
            ("Finished", "Setup is complete."),
        ]

        self.page_index = index
        self.header_title.set(titles[index][0])
        self.header_subtitle.set(titles[index][1])

        for page in self.pages:
            page.pack_forget()
        self.pages[index].pack(fill="both", expand=True)

        if index == 0:
            self.back_button.configure(state="disabled")
            self.next_button.configure(state="normal", text="Next >")
            self.cancel_button.configure(state="normal", text="Cancel")
        elif index == 1:
            self.back_button.configure(state="normal")
            self.next_button.configure(state="normal", text="Install")
            self.cancel_button.configure(state="normal", text="Cancel")
        elif index == 2:
            self.back_button.configure(state="disabled" if self.installing else "normal")
            self.next_button.configure(state="disabled" if self.installing else "normal", text="Install Again" if self.install_failed else "Install")
            self.cancel_button.configure(state="disabled" if self.installing else "normal", text="Close" if self.install_failed else "Cancel")
        else:
            self.back_button.configure(state="disabled")
            self.next_button.configure(state="normal", text="Finish")
            self.cancel_button.configure(state="disabled", text="Cancel")

    def go_back(self):
        if self.page_index == 1:
            self.show_page(0)
        elif self.page_index == 2 and not self.installing:
            self.show_page(1)

    def go_next(self):
        if self.page_index == 0:
            self.show_page(1)
        elif self.page_index == 1:
            self.start_install()
        elif self.page_index == 2 and not self.installing:
            self.start_install()
        else:
            self.root.destroy()

    def on_close(self):
        if self.installing:
            if not messagebox.askyesno(WINDOW_TITLE, "Installation is still running.\n\nClose the setup wizard anyway?", parent=self.root):
                return
        self.root.destroy()

    def start_install(self):
        install_dir = self.install_dir_var.get().strip()
        if not install_dir:
            messagebox.showwarning(WINDOW_TITLE, "Choose an installation folder first.", parent=self.root)
            return

        plan = {
            "install_dir": Path(install_dir).expanduser().resolve(),
            "add_path": self.add_path_var.get(),
            "desktop": self.desktop_var.get(),
            "python": self.python_var.get(),
            "launch": self.launch_var.get(),
        }

        self.install_failed = False
        self.warnings = []
        self.progress["value"] = 0
        self.status_var.set("Preparing installation.")
        self.finish_var.set("")
        self.finish_path_label.configure(text="")
        self.clear_log()
        self.installing = True
        self.show_page(2)
        self.back_button.configure(state="disabled")
        self.next_button.configure(state="disabled", text="Installing...")
        self.cancel_button.configure(state="disabled")

        worker = threading.Thread(target=self.run_install, args=(plan,), daemon=True)
        worker.start()

    def run_install(self, plan):
        try:
            install_dir = plan["install_dir"]
            binary_path = install_dir / self.binary_name
            steps = [
                ("Preparing install folder", lambda: self.prepare_install_dir(install_dir)),
                ("Copying application files", lambda: self.copy_payload(install_dir)),
                ("Registering application menu entry", lambda: self.safe_step("Application menu entry", lambda: self.register_application(binary_path))),
            ]

            if plan["add_path"]:
                steps.append(("Adding Gopher AI to PATH", lambda: self.safe_step("PATH update", lambda: self.add_to_path(binary_path))))
            if plan["desktop"]:
                steps.append(("Creating desktop shortcut", lambda: self.safe_step("Desktop shortcut", lambda: self.create_desktop_shortcut(binary_path))))
            if plan["python"]:
                steps.append(("Installing Python training packages", lambda: self.safe_step("Python training packages", lambda: self.install_python_packages(install_dir))))
            if plan["launch"]:
                steps.append(("Launching Gopher AI", lambda: self.safe_step("Launch Gopher AI", lambda: self.launch_app(binary_path))))

            self.progress_total = max(len(steps), 1)

            for index, (label, action) in enumerate(steps, start=1):
                self.install_queue.put(("progress", index - 1, self.progress_total, label))
                self.install_queue.put(("log", f"{label}...\n"))
                action()
                self.install_queue.put(("progress", index, self.progress_total, label))

            finish_lines = [
                "Gopher AI was installed successfully.",
                "",
                f"Location: {install_dir}",
            ]
            if self.warnings:
                finish_lines.extend(["", "Warnings:"])
                finish_lines.extend(f"- {warning}" for warning in self.warnings)

            self.install_path = install_dir
            self.install_queue.put(("done", True, "\n".join(finish_lines)))
        except Exception:
            self.install_queue.put(("log", traceback.format_exc() + "\n"))
            self.install_queue.put(("done", False, "Installation failed. Review the log and try again."))

    def prepare_install_dir(self, install_dir):
        install_dir.mkdir(parents=True, exist_ok=True)

    def copy_payload(self, install_dir):
        shutil.copy2(self.required_files["binary"], install_dir / self.binary_name)
        shutil.copy2(self.required_files["icon"], install_dir / "ai.ico")
        shutil.copy2(self.required_files["trainer"], install_dir / "train_lora.py")
        if not self.is_windows:
            os.chmod(install_dir / self.binary_name, 0o755)
            os.chmod(install_dir / "train_lora.py", 0o755)

    def safe_step(self, label, action):
        try:
            action()
        except Exception as exc:
            message = f"{label} could not be completed: {exc}"
            self.warnings.append(message)
            self.install_queue.put(("log", message + "\n"))

    def register_application(self, binary_path):
        if self.is_windows:
            start_menu = Path(os.environ.get("APPDATA", str(Path.home()))) / "Microsoft" / "Windows" / "Start Menu" / "Programs" / "Gopher AI.lnk"
            self.create_windows_shortcut(start_menu, binary_path)
            return

        applications_dir = Path.home() / ".local" / "share" / "applications"
        applications_dir.mkdir(parents=True, exist_ok=True)
        self.write_desktop_file(applications_dir / "gopher-ai.desktop", binary_path)

    def create_desktop_shortcut(self, binary_path):
        if self.is_windows:
            desktop = Path.home() / "Desktop" / "Gopher AI.lnk"
            self.create_windows_shortcut(desktop, binary_path)
            return

        desktop_dir = Path.home() / "Desktop"
        desktop_dir.mkdir(parents=True, exist_ok=True)
        desktop_file = desktop_dir / "Gopher AI.desktop"
        self.write_desktop_file(desktop_file, binary_path)
        os.chmod(desktop_file, 0o755)

    def create_windows_shortcut(self, shortcut_path, binary_path):
        shortcut_path.parent.mkdir(parents=True, exist_ok=True)
        icon_path = binary_path.parent / "ai.ico"
        shortcut_value = str(shortcut_path).replace("'", "''")
        binary_value = str(binary_path).replace("'", "''")
        working_value = str(binary_path.parent).replace("'", "''")
        icon_value = str(icon_path).replace("'", "''")
        command = [
            "powershell",
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-Command",
            (
                "$shell = New-Object -ComObject WScript.Shell; "
                f"$shortcut = $shell.CreateShortcut('{shortcut_value}'); "
                f"$shortcut.TargetPath = '{binary_value}'; "
                f"$shortcut.WorkingDirectory = '{working_value}'; "
                f"$shortcut.IconLocation = '{icon_value}'; "
                "$shortcut.Save()"
            ),
        ]
        subprocess.run(command, check=True)

    def write_desktop_file(self, desktop_path, binary_path):
        desktop_path.write_text(
            "\n".join(
                [
                    "[Desktop Entry]",
                    "Version=1.0",
                    "Type=Application",
                    "Name=Gopher AI",
                    f'Exec="{binary_path}"',
                    f"Icon={binary_path.parent / 'ai.ico'}",
                    "Terminal=false",
                    "Categories=Development;Utility;",
                ]
            )
            + "\n",
            encoding="utf-8",
        )

    def add_to_path(self, binary_path):
        if self.is_windows:
            self.add_windows_path(binary_path.parent)
            return

        local_bin = Path.home() / ".local" / "bin"
        local_bin.mkdir(parents=True, exist_ok=True)
        link_path = local_bin / "gopher-ai"
        if link_path.exists() or link_path.is_symlink():
            link_path.unlink()
        link_path.symlink_to(binary_path)

        export_line = 'export PATH="$HOME/.local/bin:$PATH"'
        for profile in [Path.home() / ".profile", Path.home() / ".bashrc", Path.home() / ".zshrc"]:
            self.ensure_line(profile, export_line)

    def add_windows_path(self, install_dir):
        if winreg is None:
            raise RuntimeError("winreg is not available")

        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, "Environment", 0, winreg.KEY_READ | winreg.KEY_WRITE) as key:
            try:
                current_path = winreg.QueryValueEx(key, "Path")[0]
            except FileNotFoundError:
                current_path = ""

            parts = [part for part in current_path.split(";") if part]
            target = str(install_dir)
            lowered = {part.casefold() for part in parts}
            if target.casefold() not in lowered:
                parts.append(target)
                updated = ";".join(parts)
                winreg.SetValueEx(key, "Path", 0, winreg.REG_EXPAND_SZ, updated)
                os.environ["Path"] = updated
                self.broadcast_environment_change()

    def broadcast_environment_change(self):
        import ctypes

        hwnd_broadcast = 0xFFFF
        wm_settingchange = 0x001A
        smto_abortifhung = 0x0002
        result = ctypes.c_void_p()
        ctypes.windll.user32.SendMessageTimeoutW(hwnd_broadcast, wm_settingchange, 0, "Environment", smto_abortifhung, 5000, ctypes.byref(result))

    def ensure_line(self, path, line):
        path.touch(exist_ok=True)
        lines = path.read_text(encoding="utf-8").splitlines()
        if line not in lines:
            with path.open("a", encoding="utf-8") as handle:
                if lines:
                    handle.write("\n")
                handle.write(line + "\n")

    def install_python_packages(self, install_dir):
        base_python = self.resolve_python_executable()
        runtime_dir = install_dir / "python-runtime"
        runtime_python = self.runtime_python_path(runtime_dir)

        self.run_logged_process([str(base_python), "-m", "venv", str(runtime_dir)], cwd=install_dir)

        if not runtime_python.exists():
            raise RuntimeError(f"Python runtime was not created at {runtime_python}")

        self.run_logged_process([str(runtime_python), "-m", "ensurepip", "--upgrade"], cwd=install_dir)
        self.run_logged_process([str(runtime_python), "-m", "pip", "install", "--upgrade", "pip", "setuptools", "wheel"], cwd=install_dir)
        self.run_logged_process([str(runtime_python), "-m", "pip", "install", "torch", "transformers", "peft"], cwd=install_dir)

    def resolve_python_executable(self):
        python_executable = Path(sys.executable)
        if python_executable.name.lower() == "pythonw.exe":
            candidate = python_executable.with_name("python.exe")
            if candidate.exists():
                return candidate
        return python_executable

    def runtime_python_path(self, runtime_dir):
        if self.is_windows:
            return runtime_dir / "Scripts" / "python.exe"
        preferred = runtime_dir / "bin" / "python3"
        if preferred.exists():
            return preferred
        return runtime_dir / "bin" / "python"

    def run_logged_process(self, command, cwd):
        process = subprocess.Popen(
            command,
            cwd=str(cwd),
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        if process.stdout:
            for line in process.stdout:
                self.install_queue.put(("log", line))
        exit_code = process.wait()
        if exit_code != 0:
            raise RuntimeError(f"{Path(command[0]).name} exited with code {exit_code}")

    def launch_app(self, binary_path):
        subprocess.Popen([str(binary_path)], cwd=str(binary_path.parent))

    def clear_log(self):
        self.log_box.configure(state="normal")
        self.log_box.delete("1.0", "end")
        self.log_box.configure(state="disabled")

    def append_log(self, text):
        self.log_box.configure(state="normal")
        self.log_box.insert("end", text)
        self.log_box.see("end")
        self.log_box.configure(state="disabled")

    def poll_queue(self):
        try:
            while True:
                event = self.install_queue.get_nowait()
                kind = event[0]

                if kind == "log":
                    self.append_log(event[1])
                elif kind == "progress":
                    current, total, label = event[1], event[2], event[3]
                    value = int((current / max(total, 1)) * 100)
                    self.progress["value"] = value
                    self.status_var.set(label)
                elif kind == "done":
                    success, message = event[1], event[2]
                    self.installing = False
                    self.install_failed = not success
                    self.progress["value"] = 100 if success else self.progress["value"]
                    if success:
                        self.finish_var.set(message)
                        if self.install_path:
                            self.finish_path_label.configure(
                                text=(
                                    "If you added Gopher AI to PATH, open a new terminal after setup.\n"
                                    f"Installed to: {self.install_path}"
                                )
                            )
                        self.show_page(3)
                    else:
                        self.status_var.set(message)
                        self.show_page(2)
                else:
                    self.append_log(repr(event) + "\n")
        except queue.Empty:
            pass

        self.root.after(100, self.poll_queue)

    def run(self):
        self.root.mainloop()


def main():
    wizard = InstallerWizard()
    wizard.run()


if __name__ == "__main__":
    main()
