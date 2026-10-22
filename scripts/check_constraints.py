import os
import sys

def check_file(path):
    with open(path, 'r', encoding='utf-8') as f:
        lines = f.readlines()
    
    total_lines = len(lines)
    file_errs = []
    if total_lines > 500:
        file_errs.append(f"File has {total_lines} lines (> 500)")
    
    in_func = False
    func_name = ""
    func_start = 0
    brace_depth = 0
    
    for idx, line in enumerate(lines):
        stripped = line.strip()
        if not in_func and (line.startswith("func ") or line.startswith("func (")):
            in_func = True
            func_name = line.split('{')[0].strip()
            func_start = idx + 1
            brace_depth = line.count('{') - line.count('}')
            if brace_depth == 0 and '{' in line:
                in_func = False
                continue
        elif in_func:
            brace_depth += line.count('{') - line.count('}')
            if brace_depth <= 0:
                func_len = (idx + 1) - func_start + 1
                if func_len > 50:
                    file_errs.append(f"Func '{func_name}' at line {func_start} has {func_len} lines (> 50)")
                in_func = False
                func_name = ""
    
    return file_errs

def main():
    target_files = sys.argv[1:]
    any_err = False
    for p in target_files:
        errs = check_file(p)
        if errs:
            any_err = True
            print(f"FAILED {p}:")
            for e in errs:
                print(f"  - {e}")
        else:
            print(f"PASSED {p}")
    if any_err:
        sys.exit(1)

if __name__ == "__main__":
    main()
