import os
import sys
import termios
import tty
from pathlib import Path


def _key():
    value = sys.stdin.read(1)
    if value == "\x1b":
        value += sys.stdin.read(2)
    return value


def _interactive():
    return sys.stdin.isatty() and sys.stdout.isatty()


def select_one(items, title, labels):
    if not items:
        raise RuntimeError("選択肢がありません")
    if not _interactive():
        print(title)
        for i, label in enumerate(labels, 1): print(f"  {i}: {label}")
        try:
            return items[int(input("> ")) - 1]
        except (ValueError, IndexError) as exc:
            raise RuntimeError("選択が正しくありません") from exc
    index = 0
    old = termios.tcgetattr(sys.stdin)
    try:
        tty.setcbreak(sys.stdin.fileno())
        while True:
            print("\033[2J\033[H", end="")
            print(title)
            for i, label in enumerate(labels):
                cursor = "▶" if i == index else " "
                print(f"{cursor} {label}")
            print("\n↑↓:移動  Enter:決定  q:中止")
            key = _key()
            if key in ("\x1b[A", "k"):
                index = (index - 1) % len(items)
            elif key in ("\x1b[B", "j"):
                index = (index + 1) % len(items)
            elif key in ("\r", "\n"):
                return items[index]
            elif key.lower() == "q":
                raise RuntimeError("ユーザーにより中断しました")
    finally:
        termios.tcsetattr(sys.stdin, termios.TCSADRAIN, old)


def select_many(items, title, labels):
    if not items:
        raise RuntimeError("プレイリストがありません")
    if not _interactive():
        print(title)
        for i, label in enumerate(labels, 1): print(f"  {i}: {label}")
        answer = input("> ").strip()
        if answer.lower() == "all": return items
        try:
            indexes = {int(value.strip()) for value in answer.split(",")}
            selected = [item for i, item in enumerate(items, 1) if i in indexes]
        except ValueError as exc:
            raise RuntimeError("選択形式が正しくありません") from exc
        if not selected: raise RuntimeError("プレイリストが選択されていません")
        return selected
    index, selected = 0, set()
    old = termios.tcgetattr(sys.stdin)
    try:
        tty.setcbreak(sys.stdin.fileno())
        while True:
            print("\033[2J\033[H", end="")
            print(title)
            for i, label in enumerate(labels):
                cursor = "▶" if i == index else " "
                mark = "[x]" if i in selected else "[ ]"
                print(f"{cursor} {mark} {label}")
            print("\n↑↓:移動  Space:選択  Enter:決定  a:全選択  q:中止")
            key = _key()
            if key in ("\x1b[A", "k"):
                index = (index - 1) % len(items)
            elif key in ("\x1b[B", "j"):
                index = (index + 1) % len(items)
            elif key == " ":
                selected.symmetric_difference_update({index})
            elif key.lower() == "a":
                selected = set(range(len(items)))
            elif key in ("\r", "\n"):
                if not selected: raise RuntimeError("プレイリストが選択されていません")
                return [item for i, item in enumerate(items) if i in selected]
            elif key.lower() == "q":
                raise RuntimeError("ユーザーにより中断しました")
    finally:
        termios.tcsetattr(sys.stdin, termios.TCSADRAIN, old)
