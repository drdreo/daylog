import Foundation

/// Calendar math stays in one Gregorian, local-time calendar; never add 24-hour durations.
public struct JournalCalendarMonth {
    public let calendar: Calendar
    public let start: Date

    public init(containing date: Date, timeZone: TimeZone = .current) {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        calendar.firstWeekday = 2
        self.calendar = calendar
        self.start = calendar.dateInterval(of: .month, for: date)!.start
    }

    public var key: String { String(dayKey(start).prefix(7)) }

    public var cells: [Date?] {
        let leading = (calendar.component(.weekday, from: start) - calendar.firstWeekday + 7) % 7
        let count = calendar.range(of: .day, in: .month, for: start)!.count
        let total = ((leading + count + 6) / 7) * 7
        return (0..<total).map { index in
            guard index >= leading && index < leading + count else { return nil }
            return calendar.date(byAdding: .day, value: index - leading, to: start)
        }
    }

    public func moved(by months: Int) -> Self {
        Self(containing: calendar.date(byAdding: .month, value: months, to: start)!, timeZone: calendar.timeZone)
    }

    public func dayKey(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter.string(from: date)
    }
}

public struct JournalMonthSummary: Decodable, Sendable {
    public struct DayCount: Decodable, Sendable {
        public let date: String
        public let count: Int
    }
    public let version: Int
    public let month: String
    public let days: [DayCount]

    public static func decode(_ data: Data, expectedMonth: String) throws -> Self {
        let result = try JSONDecoder().decode(Self.self, from: data)
        guard result.version == 2, result.month == expectedMonth else {
            throw ClientError("Unsupported calendar index. Update Daylog and the app together.")
        }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "yyyy-MM-dd"
        formatter.isLenient = false
        var seen = Set<String>()
        for day in result.days {
            guard day.count > 0, day.date.hasPrefix(expectedMonth + "-"),
                  let date = formatter.date(from: day.date), formatter.string(from: date) == day.date,
                  seen.insert(day.date).inserted else {
                throw ClientError("Invalid journal calendar index.")
            }
        }
        return result
    }
}
