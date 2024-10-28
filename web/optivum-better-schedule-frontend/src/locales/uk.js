export default {
	greeting: 'Привіт, світ',
	language: {
		name: 'Мова',
		select: 'Виберіть мову',
	},
	theme: {
		name: 'Тема',
		options: {
			dracula: 'Дракула',
			dark: 'Темна',
			light: 'Світла',
			auto: 'Авто',
		},
	},
	day: {
		monday: 'Понеділок',
		tuesday: 'Вівторок',
		wednesday: 'Середа',
		thursday: 'Четвер',
		friday: "П'ятниця",
		saturday: 'Субота',
		sunday: 'Неділя',
	},
	weather: {
		temperature: 'Температура',
		weather: 'Погода',
		conditions: {
			// Група 2xx: Гроза
			'thunderstorm': 'Гроза',
			'thunderstorm with light rain': 'Гроза з невеликим дощем',
			'thunderstorm with rain': 'Гроза з дощем',
			'thunderstorm with heavy rain': 'Гроза з сильним дощем',
			'light thunderstorm': 'Слабка гроза',
			'heavy thunderstorm': 'Сильна гроза',
			'ragged thunderstorm': 'Нестійка гроза',
			'thunderstorm with light drizzle': 'Гроза з легкою мрякою',
			'thunderstorm with drizzle': 'Гроза з мрякою',
			'thunderstorm with heavy drizzle': 'Гроза з сильною мрякою',

			// Група 3xx: Мряка
			'drizzle': 'Мряка',
			'light intensity drizzle': 'Слабка мряка',
			'heavy intensity drizzle': 'Сильна мряка',
			'light intensity drizzle rain': 'Слабкий дощ з мрякою',
			'drizzle rain': 'Дощ з мрякою',
			'heavy intensity drizzle rain': 'Сильний дощ з мрякою',
			'shower rain and drizzle': 'Злива і мряка',
			'heavy shower rain and drizzle': 'Сильна злива і мряка',
			'shower drizzle': 'Злива з мрякою',

			// Група 5xx: Дощ
			'rain': 'Дощ',
			'light rain': 'Слабкий дощ',
			'moderate rain': 'Помірний дощ',
			'heavy intensity rain': 'Сильний дощ',
			'very heavy rain': 'Дуже сильний дощ',
			'extreme rain': 'Екстремальний дощ',
			'freezing rain': 'Крижаний дощ',
			'light intensity shower rain': 'Слабка злива',
			'shower rain': 'Злива',
			'heavy intensity shower rain': 'Сильна злива',
			'ragged shower rain': 'Перемінна злива',

			// Група 6xx: Сніг
			'snow': 'Сніг',
			'light snow': 'Слабкий сніг',
			'heavy snow': 'Сильний сніг',
			'sleet': 'Мокрий сніг',
			'light shower sleet': 'Слабка злива з мокрим снігом',
			'shower sleet': 'Злива з мокрим снігом',
			'light rain and snow': 'Слабкий дощ зі снігом',
			'rain and snow': 'Дощ зі снігом',
			'light shower snow': 'Слабка снігова злива',
			'shower snow': 'Снігова злива',
			'heavy shower snow': 'Сильна снігова злива',

			// Група 7xx: Атмосферні явища
			'mist': 'Імла',
			'smoke': 'Дим',
			'haze': 'Мгла',
			'sand/dust whirls': 'Піщані/пилові вихори',
			'fog': 'Туман',
			'sand': 'Пісок',
			'dust': 'Пил',
			'volcanic ash': 'Вулканічний попіл',
			'squalls': 'Шквали',
			'tornado': 'Торнадо',

			// Група 800: Ясно
			'clear sky': 'Ясне небо',
			'clear': 'Ясно',

			// Група 80x: Хмари
			'few clouds': 'Невелика хмарність (11-25%)',
			'scattered clouds': 'Розсіяні хмари (25-50%)',
			'broken clouds': 'Рвані хмари (51-84%)',
			'overcast clouds': 'Похмуро (85-100%)',
			'clouds': 'Хмари',
		},
	},
	page: {
		home: 'Головна',
		divisions: 'Підрозділи',
		teachers: 'Викладачі',
		classrooms: 'Класи',
		settings: 'Налаштування',
	},
};