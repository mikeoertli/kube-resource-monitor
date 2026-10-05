# Run from the repository root with make demo-gif.
# All krm commands use synthetic data; no kubeconfig or cluster is needed.
Output assets/krm-demo.gif
Require krm
Set Shell bash
Set Width 1440
Set Height 720
Set FontSize 18
Set FontFamily "Menlo"
Set Theme "Catppuccin Mocha"
Set Padding 20
Set WindowBar Colorful
Set Framerate 12
Set TypingSpeed 30ms
Set CursorBlink false
Env PS1 "$ "
Env BASH_SILENCE_DEPRECATION_WARNING "1"

Hide
Type "unset PROMPT_COMMAND; clear"
Enter
Sleep 500ms
Show
# Workload rollups, live usage and limits.
Type "krm --demo -n prod --limits -i 2s"
Enter
Sleep 4s
# Pause, filter and drill from workload to pods and containers.
Type "p"
Sleep 1s
Type "/"
Sleep 1s
Type "storefront"
Enter
Sleep 1s
Type "c"
Sleep 1s
Type "E"
Sleep 4s
Type "q"
Sleep 3s
# Resume; change sort and show all namespaces.
Type "C"
Sleep 1s
Type "q"
Sleep 1s
Escape
Sleep 1s
Type "p"
Sleep 1s
Type "s"
Sleep 1s
Type "a"
Sleep 3s
Type "!"
Sleep 3s
Type "Q"
Sleep 500ms
Hide
Type "clear"
Enter
Sleep 300ms
Show
# Node capacity, then volume capacity and usage.
Type "krm node --demo -i 2s"
Enter
Sleep 4s
Type "E"
Sleep 2s
Type "Q"
Sleep 500ms
Hide
Type "clear"
Enter
Sleep 300ms
Show
Type "krm storage --demo -n prod"
Enter
Sleep 3s
Type "E"
Sleep 2s
Type "s"
Sleep 2s
Type "s"
Sleep 2s
Type "s"
Sleep 2s
Type "Q"
Sleep 500ms
Type "krm storage top --type pvc --demo -n prod"
Enter
Sleep 4s
Hide
Type "clear"
Enter
Sleep 300ms
Show
# Threshold notifications stay on stdout: no desktop notifications.
Type "krm notify --demo -n prod -g storage --type pvc --on 'storage>80%' --once --stdout"
Enter
Sleep 4s
Hide
Type "clear"
Enter
Sleep 300ms
Show
# Export files live in the recording wrapper's disposable working directory.
Type "krm top --demo -n prod -f checkout-api --csv samples"
Enter
Sleep 3s
Type "ls samples"
Enter
Sleep 2s
Type "cut -d, -f1-4,9,13 samples/prod__deployment__checkout-api.csv"
Enter
Sleep 3s
Hide
Type "clear"
Enter
Sleep 300ms
Show
Type "krm --demo"
Enter
Sleep 4s
Hide
Type "Q"
Sleep 500ms
