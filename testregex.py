import re
import subprocess

proc=subprocess.Popen(["find", "./", "-maxdepth", "2"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
#proc=subprocess.Popen(["find", "./.tekton"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
files, _ = proc.communicate()


for file in files.split('\n'):
    s = re.findall(r"^(?!\.gitignore)^(?<!\.tekton/)^(?!OWNERS)^(?!LICENSE)(?!\.md)(.*/)$", file[2:])
    matched = len(s) > 0
    print(s)
    print("%s, %r" % (file[2:], matched))
