package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/deploy"
)

func answer(ctx context.Context, r *bufio.Reader, w io.Writer, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Fprint(w, prompt)
	type inputResult struct {
		text string
		err  error
	}
	ready := make(chan inputResult, 1)
	// Only one prompt reads at a time. On cancellation main exits; a blocked
	// terminal read must never hold the user in the wizard after Ctrl+C.
	go func() { s, err := r.ReadString('\n'); ready <- inputResult{s, err} }()
	var s string
	var err error
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-ready:
		s, err = result.text, result.err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, io.EOF) && len(s) > 0 {
		err = nil
	}
	if err != nil {
		return "", fmt.Errorf("ввод завершён; используйте plan для неинтерактивного режима: %w", err)
	}
	return strings.TrimSpace(s), nil
}

func choose(ctx context.Context, r *bufio.Reader, w io.Writer, prompt string, n, def int) (int, error) {
	for {
		s, err := answer(ctx, r, w, fmt.Sprintf("%s [1..%d, Enter=%d]: ", prompt, n, def))
		if err != nil {
			return 0, err
		}
		if s == "" {
			return def - 1, nil
		}
		v, err := strconv.Atoi(s)
		if err == nil && v >= 1 && v <= n {
			return v - 1, nil
		}
		fmt.Fprintln(w, "Введите номер из списка.")
	}
}

func yes(ctx context.Context, r *bufio.Reader, w io.Writer, prompt string, def bool) (bool, error) {
	suffix := " [y/N]: "
	if def {
		suffix = " [Y/n]: "
	}
	for {
		s, err := answer(ctx, r, w, prompt+suffix)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "":
			return def, nil
		case "y", "yes", "д", "да":
			return true, nil
		case "n", "no", "н", "нет":
			return false, nil
		}
		fmt.Fprintln(w, "Введите y/да или n/нет.")
	}
}

func wizard(ctx context.Context, o options, in io.Reader, out, errOut io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r := bufio.NewReader(in)
	fmt.Fprintf(out, "\nLLM Launcher %s\nЛокальная модель: железо → задача → движок → модель → Docker Compose\n", version)
	if _, err := os.Stat(filepath.Join(o.dir, "plan.json")); err == nil {
		if o.dryRun || o.inventory != "" {
			return errors.New("план уже существует; для нового пробного плана выберите другой --dir")
		}
		for name := range o.explicit {
			if name != "dir" && name != "timeout" {
				return fmt.Errorf("план уже существует: --%s не изменяет его; создайте новый план в другом --dir", name)
			}
		}
		return existing(ctx, o, r, out, errOut)
	}
	if o.catalog == "" && o.inventory == "" && !o.dryRun {
		go catalog.Refresh(ctx)
	}
	fmt.Fprintln(out, "\nПроверяю систему…")
	hw, err := scan(ctx, o)
	if err != nil {
		return err
	}
	printHardware(out, hw)
	ok, err := yes(ctx, r, out, "Система определена верно?", true)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("остановлено; сохраните scan --json для разбора определения железа")
	}
	o.task, err = chooseTask(ctx, r, out)
	if err != nil {
		return err
	}
	cat, err := catalog.Load(o.catalog)
	if err != nil {
		return err
	}
	o.runtime = ""
	recs, notes, err := choices(hw, cat, o)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		printRecommendations(out, recs, notes)
		return errors.New("не найден безопасный стартовый профиль; освободите память, проверьте scan или попробуйте --cpu")
	}
	engines := runtimes(recs)
	fmt.Fprintln(out, "\nДоступные варианты запуска:")
	for j, e := range engines {
		label := "Ollama — простой локальный запуск"
		if e == "vllm" {
			label = "vLLM — сервер OpenAI API на NVIDIA GPU"
		} else if e == "llama.cpp" {
			label = "llama.cpp — запуск GGUF на CPU или NVIDIA"
		}
		fmt.Fprintf(out, "%d) %s\n", j+1, label)
	}
	j, err := choose(ctx, r, out, "Движок", len(engines), 1)
	if err != nil {
		return err
	}
	o.runtime = engines[j]
	recs, notes, err = choices(hw, cat, o)
	if err != nil {
		return err
	}
	if len(recs) > 8 {
		recs = recs[:8]
	}
	fmt.Fprintln(out, "\nПодходящие модели (первый вариант — рекомендация):")
	printRecommendations(out, recs, notes)
	j, err = choose(ctx, r, out, "Модель", len(recs), 1)
	if err != nil {
		return err
	}
	p, err := makePlan(hw, cat, recs[j], o)
	if err != nil {
		return err
	}
	printPlan(out, o.dir, p)
	fmt.Fprintln(out, "Образы и веса скачиваются при первом up. Драйверы и Docker должны быть установлены; настройки системы мастер не меняет.")
	ok, err = yes(ctx, r, out, "Сохранить этот план?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "План не сохранён.")
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := deploy.WritePlan(o.dir, p); err != nil {
		return err
	}
	if o.dryRun || o.inventory != "" {
		fmt.Fprintln(out, "Пробный план сохранён. Контейнеры и загрузки не запускались.")
		return nil
	}
	if !hw.Docker.Reachable || !hw.Docker.Compose || hw.Docker.ServerOS != "linux" {
		fmt.Fprintln(out, "План сохранён. Для запуска сначала подготовьте Docker: llm-launcher doctor")
		fmt.Fprintf(out, "После подготовки: llm-launcher up --dir %s\n", strconv.Quote(o.dir))
		return nil
	}
	ok, err = yes(ctx, r, out, "Скачать и запустить модель сейчас?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(out, "Позже: llm-launcher up --dir %s\n", strconv.Quote(o.dir))
		return nil
	}
	m := deploy.Manager{Dir: o.dir, Out: out, Err: errOut, Timeout: o.timeout}
	if err := m.Up(ctx); err != nil {
		return err
	}
	printEndpoints(out, p)
	return nil
}

func existing(ctx context.Context, o options, r *bufio.Reader, out, errOut io.Writer) error {
	p, err := deploy.ReadPlan(o.dir)
	if err != nil {
		return err
	}
	printPlan(out, o.dir, p)
	fmt.Fprintln(out, "\nНайден сохранённый проект.\n1) Посмотреть состояние\n2) Запустить / проверить готовность\n3) Тестовый запрос\n4) Показать логи\n5) Остановить (веса сохранить)\n6) Выйти")
	i, err := choose(ctx, r, out, "Действие", 6, 1)
	if err != nil {
		return err
	}
	m := deploy.Manager{Dir: o.dir, Out: out, Err: errOut, Timeout: o.timeout}
	switch i {
	case 0:
		s, e := m.Status(ctx)
		printStatus(out, s)
		return e
	case 1:
		if e := m.Up(ctx); e != nil {
			return e
		}
		printEndpoints(out, p)
	case 2:
		s, e := m.Smoke(ctx)
		fmt.Fprintf(out, "ok=%t, %s, %d мс\n%s\n", s.OK, s.Kind, s.DurationMS, s.Output)
		return e
	case 3:
		return m.Logs(ctx, false)
	case 4:
		ok, e := yes(ctx, r, out, "Остановить контейнеры этого проекта?", false)
		if e != nil {
			return e
		}
		if ok {
			return m.Down(ctx)
		}
	}
	return nil
}

func chooseTask(ctx context.Context, r *bufio.Reader, out io.Writer) (string, error) {
	fmt.Fprintln(out, "\nДля чего нужна модель?\n1) Общение и работа с текстом\n2) Программирование\n3) Ответы по документам: модель для RAG\n4) Эмбеддинги для семантического поиска")
	i, err := choose(ctx, r, out, "Задача", 4, 1)
	if err != nil {
		return "", err
	}
	return []string{"chat", "code", "rag", "embeddings"}[i], nil
}
