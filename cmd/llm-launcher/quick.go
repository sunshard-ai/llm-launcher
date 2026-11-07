package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/deploy"
	"llmlauncher/internal/recommend"
)

func quickScan(ctx context.Context, o options, in io.Reader, out, errOut io.Writer) error {
	if !o.explicit["dir"] {
		o.dir = "./runs"
	}
	if o.catalog == "" && o.inventory == "" && !o.dryRun {
		go catalog.Refresh(ctx)
	}
	hw, err := scan(ctx, o)
	if err != nil {
		return err
	}
	printHardware(out, hw)
	reader := bufio.NewReader(in)
	o.task, err = chooseTask(ctx, reader, out)
	if err != nil {
		return err
	}
	cat, err := catalog.Load(o.catalog)
	if err != nil {
		return err
	}
	recs, notes, err := choices(hw, cat, o)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		printRecommendations(out, recs, notes)
		return errors.New("не найден подходящий профиль; проверьте вывод scan и доступную память")
	}

	fmt.Fprintln(out, "\nВыберите модель. Подходящий движок уже указан рядом.")
	for i, r := range recs {
		device := "CPU"
		if r.GPU != nil {
			device = r.GPU.Name
		}
		fmt.Fprintf(out, "%d) %s | %s · %s | диск ≈%.0f ГиБ с образом | %s\n", i+1, r.Model.Name,
			r.Model.Runtime, device, recommend.RequiredDiskGiB(r.Model), r.Model.License)
		if profile, ok := cat.Profiles[r.Model.ID]; ok {
			fmt.Fprintf(out, "   %s\n", profile.Purpose)
		}
	}
	exit := len(recs) + 1
	fmt.Fprintf(out, "%d) Выйти\n", exit)
	action := "Модель для скачивания и запуска"
	if o.dryRun || o.inventory != "" {
		action = "Модель для пробного плана"
	}
	index, err := choose(ctx, reader, out, action, exit, exit)
	if err != nil || index == exit-1 {
		return err
	}

	selected := recs[index]
	if !o.explicit["dir"] {
		o.dir = filepath.Join(o.dir, selected.Model.ID)
	}
	p, err := deploy.ReadPlan(o.dir)
	if errors.Is(err, os.ErrNotExist) {
		p, err = makePlan(hw, cat, selected, o)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deploy.WritePlan(o.dir, p); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if o.dryRun || o.inventory != "" {
			return errors.New("план уже существует; для пробного плана выберите другой --dir")
		}
		if p.Model.ID != selected.Model.ID || p.Model.Ref != selected.Model.Ref || p.Runtime != selected.Model.Runtime {
			return errors.New("в папке уже сохранён план другой модели; выберите новый --dir")
		}
		if (o.explicit["context"] && p.Context != o.context) ||
			(o.port != 0 && p.Port != o.port) ||
			(o.cpu && p.Acceleration != "cpu") ||
			(o.gpu != "" && (selected.GPU == nil || len(p.DeviceIDs) != 1 ||
				(p.DeviceIDs[0] != selected.GPU.UUID && p.DeviceIDs[0] != selected.GPU.Index))) {
			return errors.New("параметры отличаются от сохранённого плана; создайте новый план в другом --dir")
		}
		fmt.Fprintln(out, "Использую сохранённый план и его кэш весов.")
	}
	fmt.Fprintf(out, "Выбрано: %s · %s · контекст %d токенов\nПлан: %s\n", p.Model.Name, p.Runtime, p.Context, o.dir)
	if o.dryRun || o.inventory != "" {
		fmt.Fprintln(out, "Пробный план сохранён.")
		return nil
	}
	if !hw.Docker.Reachable || !hw.Docker.Compose || hw.Docker.ServerOS != "linux" {
		fmt.Fprintf(out, "План сохранён. После подготовки Docker выполните llm-launcher run --dir %q\n", o.dir)
		return nil
	}
	m := deploy.Manager{Dir: o.dir, Out: out, Err: errOut, Timeout: o.timeout}
	if err := m.Up(ctx); err != nil {
		return err
	}
	printEndpoints(out, p)
	return nil
}
